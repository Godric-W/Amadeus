package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
)

func TestStorePersistsRunMessagesAndInterruptedContinuation(t *testing.T) {
	store := openTestStore(t)
	startedAt := time.Unix(100, 0).UTC()
	first, err := store.BeginFirstRun(context.Background(), sqliteFirstRunInput(t.TempDir(), "project-1", "session-1", "user-1", "run-1", startedAt))
	if err != nil {
		t.Fatalf("begin first run: %v", err)
	}
	if first.Session.NextRunSequence != 2 || first.Run.Sequence != 1 || first.Message.RunID != first.Run.ID {
		t.Fatalf("unexpected first run: %#v", first)
	}
	if _, err := store.FinishRun(context.Background(), sessiondomain.FinishRunInput{
		SessionID: first.Session.ID, RunID: first.Run.ID, RunStatus: sessiondomain.RunInterrupted, StopReason: "cancelled", FinishedAt: startedAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("finish interrupted run: %v", err)
	}
	second, err := store.BeginRun(context.Background(), sessiondomain.BeginRunInput{
		SessionID: first.Session.ID, UserMessageID: "user-2", RunID: "run-2", Objective: "continue", UserContent: "continue",
		ContextFromRunID: first.Run.ID, Provider: "openai", Model: "model", APIMode: "responses", Dialect: "openai", ExecutionMode: sessiondomain.ExecutionModePlanned, StartedAt: startedAt.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("begin continuation: %v", err)
	}
	if second.Run.Sequence != 2 || second.Message.Sequence != 2 || second.Run.ContextFromRunID != first.Run.ID || second.Run.ExecutionMode != sessiondomain.ExecutionModePlanned {
		t.Fatalf("unexpected continuation: %#v", second)
	}
	finished, err := store.FinishRun(context.Background(), sessiondomain.FinishRunInput{
		SessionID: second.Session.ID, RunID: second.Run.ID, RunStatus: sessiondomain.RunCompleted, AssistantMessageID: "assistant-2", AssistantContent: "done",
		UsageJSON: json.RawMessage(`{"input_tokens":4}`), FinishedAt: startedAt.Add(3 * time.Second),
	})
	if err != nil {
		t.Fatalf("finish continuation: %v", err)
	}
	if finished.AssistantMessage == nil || finished.AssistantMessage.RunID != second.Run.ID {
		t.Fatalf("unexpected finished run: %#v", finished)
	}
	messages, err := store.ListCompletedMessages(context.Background(), first.Session.ID)
	if err != nil || len(messages) != 2 || messages[0].RunID != second.Run.ID || messages[1].RunID != second.Run.ID {
		t.Fatalf("unexpected persisted messages: %#v err=%v", messages, err)
	}
}

func TestStoreAllocatesDistinctRunSequencesConcurrently(t *testing.T) {
	store := openTestStore(t)
	startedAt := time.Unix(100, 0).UTC()
	first, err := store.BeginFirstRun(context.Background(), sqliteFirstRunInput(t.TempDir(), "project-1", "session-1", "user-1", "run-1", startedAt))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishRun(context.Background(), sessiondomain.FinishRunInput{SessionID: first.Session.ID, RunID: first.Run.ID, RunStatus: sessiondomain.RunInterrupted, StopReason: "ready", FinishedAt: startedAt.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	sequences := make(chan int64, 3)
	errs := make(chan error, 3)
	for index := 0; index < 3; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			result, err := store.BeginRun(context.Background(), sessiondomain.BeginRunInput{
				SessionID: first.Session.ID, UserMessageID: sessiondomain.MessageID(fmt.Sprintf("user-%d", index+2)), RunID: sessiondomain.RunID(fmt.Sprintf("run-%d", index+2)),
				Objective: "continue", UserContent: "continue", ContextFromRunID: first.Run.ID, StartedAt: startedAt.Add(2 * time.Second),
			})
			if err != nil {
				errs <- err
				return
			}
			sequences <- result.Run.Sequence
		}(index)
	}
	wait.Wait()
	close(errs)
	close(sequences)
	for err := range errs {
		t.Fatalf("concurrent BeginRun failed: %v", err)
	}
	seen := map[int64]bool{}
	for sequence := range sequences {
		seen[sequence] = true
	}
	if len(seen) != 3 || !seen[2] || !seen[3] || !seen[4] {
		t.Fatalf("unexpected concurrent sequences: %#v", seen)
	}
}

func TestStoreRecoversAbandonedRunningRun(t *testing.T) {
	store := openTestStore(t)
	startedAt := time.Unix(100, 0).UTC()
	first, err := store.BeginFirstRun(context.Background(), sqliteFirstRunInput(t.TempDir(), "project-1", "session-1", "user-1", "run-1", startedAt))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverRunningRuns(context.Background(), first.Session.ID, startedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.GetRun(context.Background(), first.Run.ID)
	if err != nil || recovered.Status != sessiondomain.RunInterrupted || recovered.FinishedAt == nil || len(recovered.InterruptedContextJSON) == 0 {
		t.Fatalf("unexpected recovered Run: %#v err=%v", recovered, err)
	}
	context, err := sessiondomain.DecodePreviousWork(recovered.InterruptedContextJSON)
	if err != nil || context.Objective != first.Run.Objective {
		t.Fatalf("unexpected recovered context: %#v err=%v", context, err)
	}
}

func TestStorePersistsConversationSummary(t *testing.T) {
	store := openTestStore(t)
	startedAt := time.Unix(100, 0).UTC()
	first, err := store.BeginFirstRun(context.Background(), sqliteFirstRunInput(t.TempDir(), "project-1", "session-1", "user-1", "run-1", startedAt))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishRun(context.Background(), sessiondomain.FinishRunInput{
		SessionID: first.Session.ID, RunID: first.Run.ID, RunStatus: sessiondomain.RunCompleted,
		AssistantMessageID: "assistant-1", AssistantContent: "done", FinishedAt: startedAt.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	messages, err := store.ListCompletedMessages(context.Background(), first.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	sourceHash, err := sessiondomain.ConversationSourceHash(messages)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := sessiondomain.NewConversationSummary("summary-1", first.Session.ID, messages[0].Sequence, messages[len(messages)-1].Sequence, "summary", sourceHash, "openai", "model", startedAt.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendSummary(context.Background(), summary); err != nil {
		t.Fatal(err)
	}
	latest, err := store.LatestSummary(context.Background(), first.Session.ID)
	if err != nil || latest.ID != summary.ID || latest.Content != summary.Content {
		t.Fatalf("unexpected persisted summary: %#v err=%v", latest, err)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	database, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store, err := NewStore(database)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func sqliteFirstRunInput(projectPath string, projectID sessiondomain.ProjectID, sessionID sessiondomain.ConversationSessionID, messageID sessiondomain.MessageID, runID sessiondomain.RunID, startedAt time.Time) sessiondomain.BeginFirstRunInput {
	return sessiondomain.BeginFirstRunInput{
		ProjectID: projectID, CanonicalPath: projectPath, ProjectName: "project", SessionID: sessionID, SessionTitle: "Fix tests",
		UserMessageID: messageID, RunID: runID, Objective: "Fix tests", UserContent: "Fix tests",
		Provider: "openai", Model: "model", APIMode: "responses", Dialect: "openai", StartedAt: startedAt,
	}
}
