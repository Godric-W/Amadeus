package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryStoreCreatesFirstTurnAndCompletesConversationAtomically(t *testing.T) {
	store := NewMemoryStore()
	startedAt := time.Unix(100, 0).UTC()
	input := firstTurnInput(t, "project-1", "session-1", "turn-1", "user-1", "run-1", startedAt)
	begun, err := store.BeginFirstTurn(context.Background(), input)
	if err != nil {
		t.Fatalf("begin first turn: %v", err)
	}
	if begun.Session.NextTurnSequence != 2 || begun.Turn.Sequence != 1 || begun.Message.Sequence != 1 || begun.Message.Role != MessageUser || begun.Run.Status != RunRunning {
		t.Fatalf("unexpected first turn transaction: %#v", begun)
	}
	latest, err := store.LatestSession(context.Background(), begun.Project.ID)
	if err != nil || latest.ID != begun.Session.ID {
		t.Fatalf("latest session query failed: session=%#v err=%v", latest, err)
	}

	finishedAt := startedAt.Add(time.Second)
	finished, err := store.FinishTurn(context.Background(), FinishTurnInput{
		SessionID: begun.Session.ID, TurnID: begun.Turn.ID, RunID: begun.Run.ID,
		TurnStatus: TurnCompleted, RunStatus: RunCompleted,
		AssistantMessageID: "assistant-1", AssistantContent: "Completed successfully.",
		UsageJSON: json.RawMessage(`{"input_tokens":10,"output_tokens":2}`), FinishedAt: finishedAt,
	})
	if err != nil {
		t.Fatalf("finish completed turn: %v", err)
	}
	if finished.Turn.Status != TurnCompleted || finished.Run.Status != RunCompleted || finished.AssistantMessage == nil || finished.AssistantMessage.Sequence != 2 {
		t.Fatalf("unexpected completed transaction: %#v", finished)
	}
	messages, err := store.ListMessages(context.Background(), begun.Session.ID)
	if err != nil || len(messages) != 2 || messages[0].Role != MessageUser || messages[1].Role != MessageAssistant || messages[1].Content != "Completed successfully." {
		t.Fatalf("unexpected formal conversation: messages=%#v err=%v", messages, err)
	}
	storedRun, err := store.GetRun(context.Background(), begun.Run.ID)
	if err != nil || storedRun.FinishedAt == nil || storedRun.Status != RunCompleted || string(storedRun.UsageJSON) != `{"input_tokens":10,"output_tokens":2}` {
		t.Fatalf("unexpected stored run: run=%#v err=%v", storedRun, err)
	}
}

func TestMemoryStoreCancellationKeepsUserMessageAndFindsLatestInterruptedRun(t *testing.T) {
	store := NewMemoryStore()
	startedAt := time.Unix(100, 0).UTC()
	first, err := store.BeginFirstTurn(context.Background(), firstTurnInput(t, "project-1", "session-1", "turn-1", "user-1", "run-1", startedAt))
	if err != nil {
		t.Fatalf("begin first turn: %v", err)
	}
	if _, err := store.FinishTurn(context.Background(), FinishTurnInput{
		SessionID: first.Session.ID, TurnID: first.Turn.ID, RunID: first.Run.ID,
		TurnStatus: TurnCancelled, RunStatus: RunCancelled, StopReason: "user interrupt", FinishedAt: startedAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("cancel first turn: %v", err)
	}
	messages, err := store.ListMessages(context.Background(), first.Session.ID)
	if err != nil || len(messages) != 1 || messages[0].Role != MessageUser {
		t.Fatalf("cancelled turn persisted unfinished assistant: messages=%#v err=%v", messages, err)
	}
	interrupted, err := store.LatestInterruptedRun(context.Background(), first.Session.ID)
	if err != nil || interrupted.ID != first.Run.ID || interrupted.StopReason != "user interrupt" {
		t.Fatalf("latest interrupted run failed: run=%#v err=%v", interrupted, err)
	}

	second, err := store.BeginTurn(context.Background(), BeginTurnInput{
		SessionID: first.Session.ID, TurnID: "turn-2", UserMessageID: "user-2", RunID: "run-2",
		Objective: "Continue safely", UserContent: "Please continue", ContextFromRunID: first.Run.ID,
		Provider: "openai", Model: "model", APIMode: "responses", Dialect: "openai",
		BudgetJSON: json.RawMessage(`{"max_steps":10}`), StartedAt: startedAt.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("begin turn from interrupted context: %v", err)
	}
	if second.Turn.Sequence != 2 || second.Message.Sequence != 2 || second.Run.ContextFromRunID != first.Run.ID {
		t.Fatalf("unexpected resumed transaction: %#v", second)
	}
	if _, err := store.FinishTurn(context.Background(), FinishTurnInput{
		SessionID: second.Session.ID, TurnID: second.Turn.ID, RunID: second.Run.ID,
		TurnStatus: TurnCancelled, RunStatus: RunCancelled, StopReason: "second interrupt", FinishedAt: startedAt.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("cancel second turn: %v", err)
	}
	interrupted, err = store.LatestInterruptedRun(context.Background(), first.Session.ID)
	if err != nil || interrupted.ID != second.Run.ID {
		t.Fatalf("newer interruption did not replace latest query: run=%#v err=%v", interrupted, err)
	}
}

func TestMemoryStoreReturnsLatestActiveSessionForProject(t *testing.T) {
	store := NewMemoryStore()
	root := filepath.Clean(t.TempDir())
	firstInput := firstTurnInputWithPath("project-1", root, "session-1", "turn-1", "user-1", "run-1", time.Unix(100, 0).UTC())
	first, err := store.BeginFirstTurn(context.Background(), firstInput)
	if err != nil {
		t.Fatalf("begin first session: %v", err)
	}
	secondInput := firstTurnInputWithPath("unused-project-id", root, "session-2", "turn-2", "user-2", "run-2", time.Unix(200, 0).UTC())
	second, err := store.BeginFirstTurn(context.Background(), secondInput)
	if err != nil {
		t.Fatalf("begin second session: %v", err)
	}
	if second.Project.ID != first.Project.ID {
		t.Fatalf("canonical project path created a duplicate project: first=%#v second=%#v", first.Project, second.Project)
	}
	latest, err := store.LatestSession(context.Background(), first.Project.ID)
	if err != nil || latest.ID != second.Session.ID {
		t.Fatalf("latest session mismatch: session=%#v err=%v", latest, err)
	}
	sessions, err := store.ListSessions(context.Background(), first.Project.ID)
	if err != nil || len(sessions) != 2 || sessions[0].ID != second.Session.ID || sessions[1].ID != first.Session.ID {
		t.Fatalf("session ordering mismatch: sessions=%#v err=%v", sessions, err)
	}
}

func TestMemoryStoreRejectsInvalidTransactionsWithoutPartialMutation(t *testing.T) {
	store := NewMemoryStore()
	input := firstTurnInput(t, "project-1", "session-1", "turn-1", "user-1", "run-1", time.Unix(100, 0).UTC())
	input.UserContent = ""
	if _, err := store.BeginFirstTurn(context.Background(), input); err == nil {
		t.Fatal("invalid first turn succeeded")
	}
	if _, err := store.GetProjectByCanonicalPath(context.Background(), input.CanonicalPath); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid first turn partially created project: %v", err)
	}

	input.UserContent = "Fix tests"
	begun, err := store.BeginFirstTurn(context.Background(), input)
	if err != nil {
		t.Fatalf("begin valid first turn: %v", err)
	}
	if _, err := store.FinishTurn(context.Background(), FinishTurnInput{
		SessionID: begun.Session.ID, TurnID: begun.Turn.ID, RunID: begun.Run.ID,
		TurnStatus: TurnCompleted, RunStatus: RunFailed, AssistantMessageID: "assistant-1",
		AssistantContent: "must not commit", FinishedAt: time.Unix(101, 0).UTC(),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("mismatched terminal statuses were accepted: %v", err)
	}
	storedRun, _ := store.GetRun(context.Background(), begun.Run.ID)
	messages, _ := store.ListMessages(context.Background(), begun.Session.ID)
	if storedRun.Status != RunRunning || len(messages) != 1 {
		t.Fatalf("failed finish partially mutated records: run=%#v messages=%#v", storedRun, messages)
	}
}

func TestMemoryStoreAppendsImmutableCheckpointSequence(t *testing.T) {
	store := NewMemoryStore()
	begun, err := store.BeginFirstTurn(context.Background(), firstTurnInput(t, "project-1", "session-1", "turn-1", "user-1", "run-1", time.Unix(100, 0).UTC()))
	if err != nil {
		t.Fatalf("begin first turn: %v", err)
	}
	payload := json.RawMessage(`{"status":"running"}`)
	checkpoint := Checkpoint{
		ID: "checkpoint-1", RunID: begun.Run.ID, Sequence: 1, SchemaVersion: 1,
		Reason: CheckpointRunStarted, PayloadJSON: payload, PayloadHash: hashPayload(payload), CreatedAt: time.Unix(101, 0).UTC(),
	}
	stored, err := store.AppendCheckpoint(context.Background(), AppendCheckpointInput{Checkpoint: checkpoint})
	if err != nil {
		t.Fatalf("append checkpoint: %v", err)
	}
	stored.PayloadJSON[0] = '['
	checkpoints, err := store.ListCheckpoints(context.Background(), begun.Run.ID)
	if err != nil || len(checkpoints) != 1 || string(checkpoints[0].PayloadJSON) != `{"status":"running"}` {
		t.Fatalf("checkpoint storage was mutable: checkpoints=%#v err=%v", checkpoints, err)
	}
	if _, err := store.AppendCheckpoint(context.Background(), AppendCheckpointInput{Checkpoint: checkpoint}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate checkpoint was accepted: %v", err)
	}
	run, _ := store.GetRun(context.Background(), begun.Run.ID)
	if run.LatestCheckpointSequence != 1 {
		t.Fatalf("run checkpoint sequence was not advanced: %#v", run)
	}
}

func firstTurnInput(t *testing.T, projectID ProjectID, sessionID ConversationSessionID, turnID TurnID, messageID MessageID, runID RunID, startedAt time.Time) BeginFirstTurnInput {
	t.Helper()
	return firstTurnInputWithPath(projectID, filepath.Clean(t.TempDir()), sessionID, turnID, messageID, runID, startedAt)
}

func firstTurnInputWithPath(projectID ProjectID, path string, sessionID ConversationSessionID, turnID TurnID, messageID MessageID, runID RunID, startedAt time.Time) BeginFirstTurnInput {
	return BeginFirstTurnInput{
		ProjectID: projectID, CanonicalPath: path, ProjectName: "Project", SessionID: sessionID, SessionTitle: "Fix tests",
		TurnID: turnID, UserMessageID: messageID, RunID: runID, Objective: "Fix tests", UserContent: "Fix tests",
		Provider: "openai", Model: "model", APIMode: "responses", Dialect: "openai",
		BudgetJSON: json.RawMessage(`{"max_steps":10}`), StartedAt: startedAt,
	}
}

func hashPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
