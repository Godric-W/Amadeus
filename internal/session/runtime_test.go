package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/policy"
	sandboxdomain "github.com/Godric-W/Amadeus/internal/sandbox"
)

func TestSessionRuntimeOwnsCanonicalHistoryAndOneActiveRun(t *testing.T) {
	runtime := newTestSessionRuntime(t)
	first, err := runtime.BeginRun(context.Background(), "inspect", RunMetadata{Mode: RunModeExecute})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.ActiveRunID() != first.Records.Run.ID || len(runtime.History().Items) != 1 {
		t.Fatalf("unexpected active runtime state: run=%q history=%#v", runtime.ActiveRunID(), runtime.History())
	}
	if _, err := runtime.BeginRun(context.Background(), "overlap", RunMetadata{}); err == nil {
		t.Fatal("overlapping Run was accepted")
	}
	payload, _ := EncodePayload(RunMarkerPayload{Reason: "checkpoint"})
	if _, err := runtime.Append(context.Background(), first.Records.Run.ID, AppendItem{ID: "item-extra", RunID: first.Records.Run.ID, Kind: RolloutContextSnapshot, Payload: payload, CreatedAt: time.Date(2026, 8, 5, 0, 0, 2, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.FinishRun(context.Background(), first, RunCompleted, "", "done", nil); err != nil {
		t.Fatal(err)
	}
	view := runtime.History()
	if runtime.ActiveRunID() != "" || len(view.Items) != 3 || view.Items[2].Kind != RolloutAssistantMessage {
		t.Fatalf("unexpected finished runtime state: run=%q history=%#v", runtime.ActiveRunID(), view)
	}
}

func TestSessionRuntimeCancelsOnlyAttachedActiveRun(t *testing.T) {
	runtime := newTestSessionRuntime(t)
	started, err := runtime.BeginRun(context.Background(), "work", RunMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	if err := runtime.AttachCancel(started.Records.Run.ID, cancel); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("user interrupt")
	if !runtime.CancelActive(cause) || !errors.Is(context.Cause(ctx), cause) {
		t.Fatalf("active Run was not cancelled with cause: %v", context.Cause(ctx))
	}
}

func TestSessionRuntimeRenameClearResumeCompactAndDelete(t *testing.T) {
	runtime := newTestSessionRuntime(t)
	started, err := runtime.BeginRun(context.Background(), "inspect project", RunMetadata{Mode: RunModeExecute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.FinishRun(context.Background(), started, RunCompleted, "", "inspection complete", nil); err != nil {
		t.Fatal(err)
	}
	sessionID := started.Records.Session.ID
	if renamedID, title, err := runtime.RenameCurrent(context.Background(), "  Project Review  "); err != nil || renamedID != sessionID || title != "Project Review" {
		t.Fatalf("rename current: id=%q title=%q err=%v", renamedID, title, err)
	}
	if runtime.History().Session.Title != "Project Review" {
		t.Fatalf("history title was not updated: %#v", runtime.History().Session)
	}
	if err := runtime.NewDraft(); err != nil {
		t.Fatal(err)
	}
	if runtime.CurrentSessionID() != "" || len(runtime.History().Items) != 0 {
		t.Fatalf("new draft retained current state: session=%q history=%#v", runtime.CurrentSessionID(), runtime.History())
	}
	sessions, err := runtime.ListSessions(context.Background())
	if err != nil || len(sessions) != 1 || sessions[0].ID != sessionID {
		t.Fatalf("old Session was not resumable: sessions=%#v err=%v", sessions, err)
	}
	if _, err := runtime.Resume(context.Background(), sessionID); err != nil {
		t.Fatal(err)
	}
	projection, err := ProjectMessages(runtime.History().Items)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(projection.Messages)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	payload, err := EncodePayload(ContextCompactionPayload{
		Summary:                "Project inspection completed.",
		ReplacementHistory:     []CompactionHistoryItem{{Role: llm.RoleAssistant, Content: "Project inspection completed."}},
		CoveredThroughSequence: projection.SourceSequences[len(projection.SourceSequences)-1], SourceHash: hex.EncodeToString(digest[:]),
		Provider: "mock", Model: "mock-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := runtime.AppendStandalone(context.Background(), AppendItem{ID: "item-compaction", Kind: RolloutContextCompaction, Payload: payload, CreatedAt: time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)})
	if err != nil || len(items) != 1 {
		t.Fatalf("append standalone compaction: items=%#v err=%v", items, err)
	}
	compacted, err := ProjectMessages(runtime.History().Items)
	if err != nil || len(compacted.Messages) != 1 || compacted.Messages[0].Content != "Project inspection completed." {
		t.Fatalf("compaction was not applied: projection=%#v err=%v", compacted, err)
	}
	deleted, err := runtime.DeleteCurrent(context.Background())
	if err != nil || deleted != sessionID || runtime.CurrentSessionID() != "" {
		t.Fatalf("delete current: deleted=%q current=%q err=%v", deleted, runtime.CurrentSessionID(), err)
	}
	sessions, err = runtime.ListSessions(context.Background())
	if err != nil || len(sessions) != 0 {
		t.Fatalf("deleted Session remained listed: sessions=%#v err=%v", sessions, err)
	}
}

func TestSessionRuntimeRejectsMutationsDuringActiveRun(t *testing.T) {
	runtime := newTestSessionRuntime(t)
	if _, err := runtime.BeginRun(context.Background(), "active", RunMetadata{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.RenameCurrent(context.Background(), "blocked"); err == nil {
		t.Fatal("rename during active Run unexpectedly succeeded")
	}
	if _, err := runtime.DeleteCurrent(context.Background()); err == nil {
		t.Fatal("delete during active Run unexpectedly succeeded")
	}
	if err := runtime.NewDraft(); err == nil {
		t.Fatal("new Draft during active Run unexpectedly succeeded")
	}
	if _, err := runtime.AppendStandalone(context.Background(), AppendItem{}); err == nil {
		t.Fatal("standalone append during active Run unexpectedly succeeded")
	}
}

func TestSessionRuntimeOwnsExtensionLifecycle(t *testing.T) {
	closer := &countingSessionCloser{}
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	coordinator, err := NewCoordinator(NewMemoryStore(), t.TempDir(), "project", CoordinatorOptions{
		IDFactory: func(kind string) string { return kind + "-1" },
		Clock:     func() time.Time { now = now.Add(time.Second); return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewSessionRuntimeWithOptions(coordinator, SessionRuntimeOptions{Extensions: closer})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if closer.calls != 1 {
		t.Fatalf("extension close calls = %d, want 1", closer.calls)
	}
}

func TestSessionRuntimeReplacesExtensionsWhenSessionChanges(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	sequence := 0
	coordinator, err := NewCoordinator(store, t.TempDir(), "project", CoordinatorOptions{
		IDFactory: func(kind string) string { sequence++; return fmt.Sprintf("%s-%d", kind, sequence) },
		Clock:     func() time.Time { now = now.Add(time.Second); return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	var closers []*countingSessionCloser
	runtime, err := NewSessionRuntimeWithOptions(coordinator, SessionRuntimeOptions{ExtensionFactory: func() (io.Closer, error) {
		closer := &countingSessionCloser{}
		closers = append(closers, closer)
		return closer, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := runtime.BeginRun(context.Background(), "first", RunMetadata{Mode: RunModeExecute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.FinishRun(context.Background(), first, RunCompleted, "", "done", nil); err != nil {
		t.Fatal(err)
	}
	grantedRoot := t.TempDir()
	if err := runtime.PermissionStore().GrantWritableRoots([]string{grantedRoot}); err != nil {
		t.Fatal(err)
	}
	key, ok := policy.NewCommandApprovalKey("/bin/sh", "echo ok", first.Records.Project.CanonicalPath, false, sandboxdomain.IsolationUnsandboxed)
	if !ok {
		t.Fatal("create Session approval key")
	}
	runtime.ApprovalStore().Approve(key)
	second, err := NewSession("session-other", first.Records.Project.ID, "other", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	store.mutex.Lock()
	store.sessions[second.ID] = second
	store.items[second.ID] = nil
	store.mutex.Unlock()
	if _, err := runtime.Resume(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}
	if len(runtime.PermissionStore().Snapshot().WritableRoots) != 0 || runtime.ApprovalStore().IsApproved(key) {
		t.Fatal("Session permission or approval stores survived a Session switch")
	}
	if len(closers) != 2 || closers[0].calls != 1 || closers[1].calls != 0 {
		t.Fatalf("unexpected Session extension replacement: %#v", closers)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if closers[1].calls != 1 {
		t.Fatalf("active Session extension was not closed: %#v", closers)
	}
}

type countingSessionCloser struct{ calls int }

func (closer *countingSessionCloser) Close() error {
	closer.calls++
	return nil
}

func TestSessionRuntimeFinishRepairsPendingToolCall(t *testing.T) {
	runtime := newTestSessionRuntime(t)
	started, err := runtime.BeginRun(context.Background(), "inspect", RunMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := EncodePayload(ToolCallPayload{Calls: []ToolCallRecord{{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"README.md"}`)}}})
	if _, err := runtime.Append(context.Background(), started.Records.Run.ID, AppendItem{
		ID: "item-call", RunID: started.Records.Run.ID, Kind: RolloutToolCall, Payload: payload,
		CreatedAt: time.Date(2026, 8, 5, 0, 0, 1, 500_000_000, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.FinishRun(context.Background(), started, RunInterrupted, "user cancelled", "", nil); err != nil {
		t.Fatal(err)
	}
	items := runtime.History().Items
	if len(items) != 4 || items[2].Kind != RolloutToolResult || items[3].Kind != RolloutRunInterrupted {
		t.Fatalf("unexpected interrupted rollout: %#v", items)
	}
	result, err := DecodeToolResult(items[2])
	if err != nil || result.Status != "interrupted" || result.CallID != "call-1" {
		t.Fatalf("unexpected interrupted Tool Result: %#v err=%v", result, err)
	}
}

func TestSessionHistoryProjectsCanonicalToolProtocolInOrder(t *testing.T) {
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	value, err := NewSession("session-1", "project-1", "history", now)
	if err != nil {
		t.Fatal(err)
	}
	items := []RolloutItem{
		rolloutItem(t, "user", value.ID, "run-1", 1, RolloutUserMessage, UserMessagePayload{Content: "inspect"}, now),
		rolloutItem(t, "calls", value.ID, "run-1", 2, RolloutToolCall, ToolCallPayload{ResponseID: "response-1", Content: "checking", Calls: []ToolCallRecord{
			{ID: "call-b", Name: "read_file", Arguments: json.RawMessage(`{"path":"b.go"}`)},
			{ID: "call-a", Name: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)},
		}}, now.Add(time.Second)),
		rolloutItem(t, "result-b", value.ID, "run-1", 3, RolloutToolResult, ToolResultPayload{CallID: "call-b", ToolName: "read_file", Status: "succeeded", Text: "B"}, now.Add(2*time.Second)),
		rolloutItem(t, "result-a", value.ID, "run-1", 4, RolloutToolResult, ToolResultPayload{CallID: "call-a", ToolName: "read_file", Status: "failed", Error: &ToolErrorPayload{Kind: "tool", Message: "missing"}}, now.Add(3*time.Second)),
	}
	history, err := NewSessionHistory(value, items)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := history.ProjectMessages()
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Messages) != 4 || projection.Messages[1].Role != llm.RoleAssistant || len(projection.Messages[1].ToolCalls) != 2 {
		t.Fatalf("unexpected projection: %#v", projection)
	}
	if projection.Messages[1].ToolCalls[0].ID != "call-b" || projection.Messages[2].ToolCallID != "call-b" || projection.Messages[3].ToolCallID != "call-a" {
		t.Fatalf("tool protocol order changed: %#v", projection.Messages)
	}
	if projection.SourceSequences[0] != 1 || projection.SourceSequences[3] != 4 {
		t.Fatalf("unexpected source sequences: %#v", projection.SourceSequences)
	}
}

func TestSessionHistoryAppliesLatestValidCompactionAndTail(t *testing.T) {
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	value, err := NewSession("session-1", "project-1", "history", now)
	if err != nil {
		t.Fatal(err)
	}
	first := rolloutItem(t, "user-1", value.ID, "run-1", 1, RolloutUserMessage, UserMessagePayload{Content: "old goal"}, now)
	second := rolloutItem(t, "assistant-1", value.ID, "run-1", 2, RolloutAssistantMessage, AssistantMessagePayload{Content: "old answer"}, now.Add(time.Second))
	source := []llm.Message{llm.UserMessage("old goal"), llm.AssistantMessage("old answer")}
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	compaction := rolloutItem(t, "compact-1", value.ID, "run-1", 3, RolloutContextCompaction, ContextCompactionPayload{
		Summary: "summary", ReplacementHistory: []CompactionHistoryItem{
			{Role: llm.RoleUser, Content: "condensed goal"},
			{Role: llm.RoleAssistant, Content: "condensed state"},
		},
		CoveredThroughSequence: 2, SourceHash: hex.EncodeToString(digest[:]), Provider: "openai", Model: "model",
	}, now.Add(2*time.Second))
	tail := rolloutItem(t, "user-2", value.ID, "run-2", 4, RolloutUserMessage, UserMessagePayload{Content: "continue"}, now.Add(3*time.Second))
	projection, err := ProjectMessages([]RolloutItem{first, second, compaction, tail})
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Messages) != 3 || projection.Messages[0].Content != "condensed goal" || projection.Messages[2].Content != "continue" {
		t.Fatalf("unexpected compacted projection: %#v", projection.Messages)
	}
	if projection.SourceSequences[0] != 2 || projection.SourceSequences[1] != 2 || projection.SourceSequences[2] != 4 {
		t.Fatalf("unexpected compacted source sequences: %#v", projection.SourceSequences)
	}
}

func TestSessionHistoryIgnoresCompactionWithMismatchedSourceHash(t *testing.T) {
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	value, _ := NewSession("session-1", "project-1", "history", now)
	items := []RolloutItem{
		rolloutItem(t, "user-1", value.ID, "run-1", 1, RolloutUserMessage, UserMessagePayload{Content: "old goal"}, now),
		rolloutItem(t, "compact-1", value.ID, "run-1", 2, RolloutContextCompaction, ContextCompactionPayload{
			Summary: "summary", ReplacementHistory: []CompactionHistoryItem{{Role: llm.RoleAssistant, Content: "summary"}},
			CoveredThroughSequence: 1, SourceHash: fmt.Sprintf("%064d", 0),
		}, now.Add(time.Second)),
	}
	projection, err := ProjectMessages(items)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Messages) != 1 || projection.Messages[0].Role != llm.RoleUser || projection.Messages[0].Content != "old goal" {
		t.Fatalf("invalid compaction should fall back to canonical source history: %#v", projection.Messages)
	}
}

func TestSessionHistoryProjectsInterruptedRunMarker(t *testing.T) {
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	value, _ := NewSession("session-1", "project-1", "history", now)
	marker := rolloutItem(t, "marker", value.ID, "run-1", 1, RolloutRunInterrupted, RunMarkerPayload{
		Reason: "process restarted", Guidance: "Re-plan from the current workspace state.",
	}, now)
	projection, err := ProjectMessages([]RolloutItem{marker})
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Messages) != 1 || projection.Messages[0].Role != llm.RoleDeveloper || !strings.Contains(projection.Messages[0].Content, "process restarted") {
		t.Fatalf("unexpected marker projection: %#v", projection.Messages)
	}
}

func rolloutItem(t *testing.T, id string, sessionID SessionID, runID RunID, sequence int64, kind RolloutKind, payload any, createdAt time.Time) RolloutItem {
	t.Helper()
	encoded, err := EncodePayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	item, err := NewRolloutItem(RolloutItemID(id), sessionID, runID, sequence, kind, encoded, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func newTestSessionRuntime(t *testing.T) *SessionRuntime {
	t.Helper()
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	sequence := 0
	coordinator, err := NewCoordinator(NewMemoryStore(), t.TempDir(), "project", CoordinatorOptions{
		IDFactory: func(kind string) string { sequence++; return fmt.Sprintf("%s-%d", kind, sequence) },
		Clock:     func() time.Time { now = now.Add(time.Second); return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewSessionRuntime(coordinator)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}
