package policy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type recordingApprovalHandler struct {
	mutex     sync.Mutex
	requests  []ApprovalRequest
	decisions []ApprovalDecision
	err       error
}

func (handler *recordingApprovalHandler) Decide(_ context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()
	handler.requests = append(handler.requests, request.Clone())
	if handler.err != nil {
		return ApprovalDecision{}, handler.err
	}
	index := len(handler.requests) - 1
	if index >= len(handler.decisions) {
		return ApprovalDecision{Outcome: ApprovalDeny, Scope: ApprovalOnce, Source: ApprovalSourceDefault, Reason: "test default deny"}, nil
	}
	return handler.decisions[index], nil
}

func (handler *recordingApprovalHandler) count() int {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()
	return len(handler.requests)
}

func TestToolAuthorizerRunsPathPreflightBeforeApproval(t *testing.T) {
	root := newPolicyProjectRoot(t)
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{allowOnceDecision()}}
	authorizer := newTestToolAuthorizer(t, root, handler, nil)

	err := authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("write-1", "write_file", json.RawMessage(`{"path":"../outside","content":"x"}`)))
	if err == nil || !strings.Contains(err.Error(), "path preflight") {
		t.Fatalf("unexpected path preflight result: %v", err)
	}
	if handler.count() != 0 {
		t.Fatalf("path escape reached approval handler %d time(s)", handler.count())
	}

	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(root.Path(), "escape")); err != nil {
		t.Fatalf("create escape symlink: %v", err)
	}
	err = authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("write-2", "write_file", json.RawMessage(`{"path":"escape/file.txt","content":"x"}`)))
	if err == nil || !strings.Contains(err.Error(), "outside project root") || handler.count() != 0 {
		t.Fatalf("symlink escape did not fail before approval: err=%v calls=%d", err, handler.count())
	}
}

func TestToolAuthorizerBlocksDangerousCommandBeforeApproval(t *testing.T) {
	root := newPolicyProjectRoot(t)
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{allowOnceDecision()}}
	authorizer := newTestToolAuthorizer(t, root, handler, nil)

	err := authorizer.Authorize(context.Background(), executeToolSpec(), tool.NewCall("exec-1", "execute_command", json.RawMessage(`{"command":"rm -rf /","cwd":"."}`)))
	var denied *ToolDeniedError
	if !errors.As(err, &denied) || denied.Risk != CommandRiskBlocked || denied.Source != ApprovalSourcePolicy {
		t.Fatalf("unexpected blocked command result: %#v err=%v", denied, err)
	}
	if handler.count() != 0 {
		t.Fatalf("blocked command reached approval handler %d time(s)", handler.count())
	}
}

func TestToolAuthorizerAllowsLowRiskCommandWithoutApproval(t *testing.T) {
	root := newPolicyProjectRoot(t)
	handler := &recordingApprovalHandler{}
	authorizer := newTestToolAuthorizer(t, root, handler, nil)
	if err := authorizer.Authorize(context.Background(), executeToolSpec(), tool.NewCall("exec-low", "execute_command", json.RawMessage(`{"command":"git status --short"}`))); err != nil {
		t.Fatalf("authorize low-risk command: %v", err)
	}
	if handler.count() != 0 {
		t.Fatalf("low-risk command requested approval %d time(s)", handler.count())
	}
}

func TestToolAuthorizerAppliesApprovalAndExactSessionGrant(t *testing.T) {
	root := newPolicyProjectRoot(t)
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{
		{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "approved for session"},
		{Outcome: ApprovalDeny, Scope: ApprovalOnce, Source: ApprovalSourceUser, Reason: "different operation denied"},
	}}
	grants := NewGrantCache()
	authorizer := newTestToolAuthorizer(t, root, handler, grants)
	arguments := json.RawMessage(`{"path":"result.txt","content":"same"}`)
	for _, id := range []string{"write-1", "write-2"} {
		if err := authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall(id, "write_file", arguments)); err != nil {
			t.Fatalf("authorize session-granted call %s: %v", id, err)
		}
	}
	if handler.count() != 1 {
		t.Fatalf("exact session grant did not suppress repeated approval: %d", handler.count())
	}

	err := authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("write-3", "write_file", json.RawMessage(`{"path":"result.txt","content":"different"}`)))
	var denied *ToolDeniedError
	if !errors.As(err, &denied) || denied.Source != ApprovalSourceUser || handler.count() != 2 {
		t.Fatalf("different operation reused grant: denied=%#v err=%v calls=%d", denied, err, handler.count())
	}
}

func TestToolAuthorizerPropagatesApprovalFailuresAndInvalidDecisions(t *testing.T) {
	root := newPolicyProjectRoot(t)
	expected := errors.New("handler failed")
	authorizer := newTestToolAuthorizer(t, root, &recordingApprovalHandler{err: expected}, nil)
	err := authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("write-error", "write_file", json.RawMessage(`{"path":"result.txt","content":"x"}`)))
	if !errors.Is(err, expected) {
		t.Fatalf("unexpected handler error: %v", err)
	}

	authorizer = newTestToolAuthorizer(t, root, &recordingApprovalHandler{decisions: []ApprovalDecision{{Outcome: ApprovalAllow}}}, nil)
	err = authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("write-invalid", "write_file", json.RawMessage(`{"path":"result.txt","content":"x"}`)))
	if err == nil || !strings.Contains(err.Error(), "validate tool approval decision") {
		t.Fatalf("unexpected invalid decision result: %v", err)
	}
}

func TestToolAuthorizerValidatesConstructionAndCalls(t *testing.T) {
	root := newPolicyProjectRoot(t)
	if authorizer, err := NewToolAuthorizer(root, nil, nil); err == nil || authorizer != nil {
		t.Fatalf("unexpected nil-handler constructor result: authorizer=%#v err=%v", authorizer, err)
	}
	if authorizer, err := NewToolAuthorizer(project.Root{}, &recordingApprovalHandler{}, nil); err == nil || authorizer != nil {
		t.Fatalf("unexpected empty-root constructor result: authorizer=%#v err=%v", authorizer, err)
	}

	var authorizer *ToolAuthorizer
	if err := authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("id", "write_file", json.RawMessage(`{}`))); err == nil {
		t.Fatal("nil authorizer did not fail")
	}
	authorizer = newTestToolAuthorizer(t, root, &recordingApprovalHandler{}, nil)
	if err := authorizer.Authorize(nil, writeToolSpec(), tool.NewCall("id", "write_file", json.RawMessage(`{}`))); err == nil {
		t.Fatal("nil context did not fail")
	}
	if err := authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("id", "other", json.RawMessage(`{}`))); err == nil {
		t.Fatal("mismatched call/spec did not fail")
	}
}

func newPolicyProjectRoot(t *testing.T) project.Root {
	t.Helper()
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatalf("create policy project root: %v", err)
	}
	return root
}

func newTestToolAuthorizer(t *testing.T, root project.Root, handler ApprovalHandler, grants *GrantCache) *ToolAuthorizer {
	t.Helper()
	authorizer, err := NewToolAuthorizer(root, handler, grants)
	if err != nil {
		t.Fatalf("create tool authorizer: %v", err)
	}
	return authorizer
}

func writeToolSpec() tool.Spec {
	return tool.Spec{Name: "write_file", SideEffect: tool.SideEffectWrite}
}

func executeToolSpec() tool.Spec {
	return tool.Spec{Name: "execute_command", SideEffect: tool.SideEffectExecute}
}

func allowOnceDecision() ApprovalDecision {
	return ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalOnce, Source: ApprovalSourceUser, Reason: "approved once"}
}
