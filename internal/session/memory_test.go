package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryStoreCreatesAndCompletesFirstRunAtomically(t *testing.T) {
	store := NewMemoryStore()
	startedAt := time.Unix(100, 0).UTC()
	begun, err := store.BeginFirstRun(context.Background(), firstRunInput(t.TempDir(), "project-1", "session-1", "user-1", "run-1", startedAt))
	if err != nil {
		t.Fatalf("begin first run: %v", err)
	}
	if begun.Session.NextRunSequence != 2 || begun.Run.Sequence != 1 || begun.Message.Sequence != 1 || begun.Message.RunID != begun.Run.ID || begun.Run.Status != RunRunning {
		t.Fatalf("unexpected first run transaction: %#v", begun)
	}
	finished, err := store.FinishRun(context.Background(), FinishRunInput{
		SessionID: begun.Session.ID, RunID: begun.Run.ID, RunStatus: RunCompleted,
		AssistantMessageID: "assistant-1", AssistantContent: "Completed successfully.",
		UsageJSON: json.RawMessage(`{"input_tokens":10,"output_tokens":2}`), FinishedAt: startedAt.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("finish completed run: %v", err)
	}
	if finished.Run.Status != RunCompleted || finished.AssistantMessage == nil || finished.AssistantMessage.Sequence != 2 || finished.AssistantMessage.RunID != begun.Run.ID {
		t.Fatalf("unexpected completed transaction: %#v", finished)
	}
	messages, err := store.ListCompletedMessages(context.Background(), begun.Session.ID)
	if err != nil || len(messages) != 2 || messages[0].Role != MessageUser || messages[1].Role != MessageAssistant {
		t.Fatalf("unexpected conversation messages: %#v err=%v", messages, err)
	}
}

func TestMemoryStoreInterruptedRunCreatesSequencedContinuation(t *testing.T) {
	store := NewMemoryStore()
	startedAt := time.Unix(100, 0).UTC()
	first, err := store.BeginFirstRun(context.Background(), firstRunInput(t.TempDir(), "project-1", "session-1", "user-1", "run-1", startedAt))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishRun(context.Background(), FinishRunInput{
		SessionID: first.Session.ID, RunID: first.Run.ID, RunStatus: RunInterrupted, StopReason: "user interrupt", FinishedAt: startedAt.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	second, err := store.BeginRun(context.Background(), BeginRunInput{
		SessionID: first.Session.ID, UserMessageID: "user-2", RunID: "run-2", Objective: "Continue safely", UserContent: "Please continue",
		ContextFromRunID: first.Run.ID, Provider: "openai", Model: "model", APIMode: "responses", Dialect: "openai", ExecutionMode: ExecutionModePlanned, StartedAt: startedAt.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Run.Sequence != 2 || second.Message.Sequence != 2 || second.Message.RunID != second.Run.ID || second.Run.ContextFromRunID != first.Run.ID || second.Run.ExecutionMode != ExecutionModePlanned {
		t.Fatalf("unexpected resumed run: %#v", second)
	}
	if _, err := store.FinishRun(context.Background(), FinishRunInput{
		SessionID: second.Session.ID, RunID: second.Run.ID, RunStatus: RunInterrupted, StopReason: "second interrupt", FinishedAt: startedAt.Add(3 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	interrupted, err := store.LatestInterruptedRun(context.Background(), first.Session.ID)
	if err != nil || interrupted.ID != second.Run.ID {
		t.Fatalf("unexpected latest interrupted run: %#v err=%v", interrupted, err)
	}
}

func TestMemoryStoreRejectsInvalidRunTransitionsAndContexts(t *testing.T) {
	store := NewMemoryStore()
	startedAt := time.Unix(100, 0).UTC()
	first, err := store.BeginFirstRun(context.Background(), firstRunInput(t.TempDir(), "project-1", "session-1", "user-1", "run-1", startedAt))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginRun(context.Background(), BeginRunInput{
		SessionID: first.Session.ID, UserMessageID: "user-2", RunID: "run-2", Objective: "continue", UserContent: "continue", ContextFromRunID: first.Run.ID, StartedAt: startedAt.Add(time.Second),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("running Run became context: %v", err)
	}
	if _, err := store.FinishRun(context.Background(), FinishRunInput{SessionID: first.Session.ID, RunID: first.Run.ID, RunStatus: RunCompleted, FinishedAt: startedAt.Add(time.Second)}); err == nil {
		t.Fatal("completed Run accepted missing assistant response")
	}
}

func TestMemoryStorePreservesProjectIdentityAcrossFirstRuns(t *testing.T) {
	store := NewMemoryStore()
	path := filepath.Clean(t.TempDir())
	first, err := store.BeginFirstRun(context.Background(), firstRunInput(path, "project-1", "session-1", "user-1", "run-1", time.Unix(100, 0).UTC()))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.BeginFirstRun(context.Background(), firstRunInput(path, "unused-project", "session-2", "user-2", "run-2", time.Unix(200, 0).UTC()))
	if err != nil {
		t.Fatal(err)
	}
	if first.Project.ID != second.Project.ID {
		t.Fatalf("project identity changed: first=%#v second=%#v", first.Project, second.Project)
	}
}

func firstRunInput(projectPath string, projectID ProjectID, sessionID ConversationSessionID, messageID MessageID, runID RunID, startedAt time.Time) BeginFirstRunInput {
	return BeginFirstRunInput{
		ProjectID: projectID, CanonicalPath: filepath.Clean(projectPath), ProjectName: "project", SessionID: sessionID, SessionTitle: "Fix tests",
		UserMessageID: messageID, RunID: runID, Objective: "Fix tests", UserContent: "Fix tests",
		Provider: "openai", Model: "model", APIMode: "responses", Dialect: "openai", StartedAt: startedAt,
	}
}
