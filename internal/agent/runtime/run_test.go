package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/react"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/instruction"
	"github.com/Godric-W/Amadeus/internal/llm"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
)

func TestRunRuntimeCancelsAndFinishesExactlyOnce(t *testing.T) {
	runContext := validRunContext(t)
	var finished int
	var final FinalState
	runtime, err := NewRunRuntime(context.Background(), runContext, func(_ context.Context, value FinalState) error {
		finished++
		final = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime.State().AddUsage(llm.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5})
	if err := runtime.State().PermissionStore().GrantWritableRoots([]string{t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	var cleanupOrder []string
	if err := runtime.RegisterCleanup("first", func() error { cleanupOrder = append(cleanupOrder, "first"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RegisterCleanup("second", func() error { cleanupOrder = append(cleanupOrder, "second"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := runtime.State().AddToolCalls(2); err != nil {
		t.Fatal(err)
	}
	runtime.Cancel(errors.New("user interrupt"))
	if err := runtime.Finish(context.Background(), sessiondomain.RunInterrupted, "cancelled", ""); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Finish(context.Background(), sessiondomain.RunFailed, "ignored", ""); err != nil {
		t.Fatal(err)
	}
	<-runtime.Done()
	if finished != 1 || final.Status != sessiondomain.RunInterrupted || final.State.Usage.TotalTokens != 5 || final.State.ToolCalls != 2 || final.State.ProcessOwner != "run-1" || !final.State.Terminal {
		t.Fatalf("unexpected final Run state: calls=%d value=%#v", finished, final)
	}
	if len(cleanupOrder) != 2 || cleanupOrder[0] != "second" || cleanupOrder[1] != "first" {
		t.Fatalf("Run cleanups were not executed once in reverse ownership order: %v", cleanupOrder)
	}
	if len(runtime.State().PermissionStore().Snapshot().WritableRoots) != 0 {
		t.Fatal("Run permission store survived terminal state")
	}
}

func TestRequestContextCloneIsolatesDynamicSnapshots(t *testing.T) {
	request := RequestContext{Run: validRunContext(t), History: sessiondomain.HistoryView{Items: []sessiondomain.RolloutItem{{PayloadJSON: []byte(`{"value":1}`)}}}, SkillInjections: []SkillInjection{{Name: "review", Content: "body"}}}
	clone := request.Clone()
	clone.History.Items[0].PayloadJSON[0] = '['
	clone.SkillInjections[0].Content = "changed"
	if string(request.History.Items[0].PayloadJSON) != `{"value":1}` || request.SkillInjections[0].Content != "body" {
		t.Fatalf("request clone shares mutable storage: %#v", request)
	}
}

func TestNewRequestContextFreezesRevisionsAndRejectsSessionDrift(t *testing.T) {
	run := validRunContext(t)
	history := sessiondomain.HistoryView{Session: run.Session, Items: []sessiondomain.RolloutItem{{PayloadJSON: []byte(`{"value":1}`)}}}
	request, err := NewRequestContext(
		run, history, nil, instruction.Resolution{}, WorkspaceSnapshot{CWD: run.CWD}, run.ContextProfile,
		strings.Repeat("a", 64), strings.Repeat("b", 64), nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if request.MCPRevision != strings.Repeat("a", 64) || request.SkillRevision != strings.Repeat("b", 64) || len(request.ToolRevision) != 64 {
		t.Fatalf("RequestContext revisions were not frozen: %#v", request)
	}
	history.Session.ID = "other-session"
	if _, err := NewRequestContext(run, history, nil, instruction.Resolution{}, WorkspaceSnapshot{CWD: run.CWD}, run.ContextProfile, "", "", nil); err == nil {
		t.Fatal("RequestContext accepted history from another Session")
	}
}

func validRunContext(t *testing.T) *RunContext {
	t.Helper()
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	root := filepath.Clean(t.TempDir())
	project, err := sessiondomain.NewProject("project-1", root, "project", now)
	if err != nil {
		t.Fatal(err)
	}
	session, err := sessiondomain.NewSession("session-1", project.ID, "session", now)
	if err != nil {
		t.Fatal(err)
	}
	run, err := sessiondomain.NewRun("run-1", session.ID, 1, sessiondomain.RunModeExecute, now)
	if err != nil {
		t.Fatal(err)
	}
	value, err := NewRunContext(project, session, run, RunContext{
		Provider: "openai", Model: "model", Mode: sessiondomain.RunModeExecute, CWD: root,
		FileSystem:     FileSystemProfile{ReadHost: true, WorkspaceRoots: []string{root}},
		ContextProfile: agentcontext.DefaultContextProfile(128_000, 4096),
		Budget:         react.Budget{MaxIterations: 10, MaxToolCalls: 20, MaxInputTokens: 1000, MaxOutputTokens: 1000, MaxDuration: time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}
