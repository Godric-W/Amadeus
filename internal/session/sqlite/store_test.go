package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
)

func TestSQLiteStorePersistsCompletedAndCancelledTurns(t *testing.T) {
	root := t.TempDir()
	database, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	store, err := NewStore(database)
	if err != nil {
		t.Fatalf("create SQLite store: %v", err)
	}
	startedAt := time.Unix(100, 0).UTC()
	first, err := store.BeginFirstTurn(context.Background(), sqliteFirstInput(root, "project-1", "session-1", "turn-1", "user-1", "run-1", startedAt))
	if err != nil {
		t.Fatalf("begin first turn: %v", err)
	}
	completed, err := store.FinishTurn(context.Background(), sessiondomain.FinishTurnInput{
		SessionID: first.Session.ID, TurnID: first.Turn.ID, RunID: first.Run.ID,
		TurnStatus: sessiondomain.TurnCompleted, RunStatus: sessiondomain.RunCompleted,
		AssistantMessageID: "assistant-1", AssistantContent: "Completed.",
		UsageJSON: json.RawMessage(`{"input_tokens":10}`), FinishedAt: startedAt.Add(time.Second),
	})
	if err != nil || completed.AssistantMessage == nil || completed.AssistantMessage.Sequence != 2 {
		t.Fatalf("finish completed turn: result=%#v err=%v", completed, err)
	}
	second, err := store.BeginTurn(context.Background(), sessiondomain.BeginTurnInput{
		SessionID: first.Session.ID, TurnID: "turn-2", UserMessageID: "user-2", RunID: "run-2",
		Objective: "Second task", UserContent: "Second task", Provider: "openai", Model: "model",
		APIMode: "responses", Dialect: "openai", BudgetJSON: json.RawMessage(`{"max_steps":10}`), StartedAt: startedAt.Add(2 * time.Second),
	})
	if err != nil || second.Turn.Sequence != 2 || second.Message.Sequence != 3 {
		t.Fatalf("begin second turn: result=%#v err=%v", second, err)
	}
	if _, err := store.FinishTurn(context.Background(), sessiondomain.FinishTurnInput{
		SessionID: second.Session.ID, TurnID: second.Turn.ID, RunID: second.Run.ID,
		TurnStatus: sessiondomain.TurnCancelled, RunStatus: sessiondomain.RunCancelled,
		StopReason: "user interrupt", FinishedAt: startedAt.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("cancel second turn: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	database, err = Open(context.Background(), root)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer database.Close()
	store, _ = NewStore(database)
	messages, err := store.ListMessages(context.Background(), first.Session.ID)
	if err != nil || len(messages) != 3 || messages[0].Role != sessiondomain.MessageUser || messages[1].Role != sessiondomain.MessageAssistant || messages[2].Role != sessiondomain.MessageUser {
		t.Fatalf("unexpected durable conversation: messages=%#v err=%v", messages, err)
	}
	interrupted, err := store.LatestInterruptedRun(context.Background(), first.Session.ID)
	if err != nil || interrupted.ID != second.Run.ID || interrupted.StopReason != "user interrupt" {
		t.Fatalf("latest interrupted run mismatch: run=%#v err=%v", interrupted, err)
	}
}

func TestSQLiteStoreReusesProjectByCanonicalPathAndReturnsLatestSession(t *testing.T) {
	root := t.TempDir()
	database, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store, _ := NewStore(database)
	first, err := store.BeginFirstTurn(context.Background(), sqliteFirstInput(root, "project-1", "session-1", "turn-1", "user-1", "run-1", time.Unix(100, 0).UTC()))
	if err != nil {
		t.Fatalf("begin first session: %v", err)
	}
	second, err := store.BeginFirstTurn(context.Background(), sqliteFirstInput(root, "unused-project", "session-2", "turn-2", "user-2", "run-2", time.Unix(200, 0).UTC()))
	if err != nil {
		t.Fatalf("begin second session: %v", err)
	}
	if first.Project.ID != second.Project.ID {
		t.Fatalf("project path was not unique: first=%#v second=%#v", first.Project, second.Project)
	}
	latest, err := store.LatestSession(context.Background(), first.Project.ID)
	if err != nil || latest.ID != second.Session.ID {
		t.Fatalf("latest session mismatch: session=%#v err=%v", latest, err)
	}
	sessions, err := store.ListSessions(context.Background(), first.Project.ID)
	if err != nil || len(sessions) != 2 || sessions[0].ID != second.Session.ID {
		t.Fatalf("project sessions mismatch: sessions=%#v err=%v", sessions, err)
	}
}

func TestSQLiteStoreAllocatesTurnAndMessageSequencesAtomically(t *testing.T) {
	database, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store, _ := NewStore(database)
	startedAt := time.Unix(100, 0).UTC()
	first, err := store.BeginFirstTurn(context.Background(), sqliteFirstInput(t.TempDir(), "project-1", "session-1", "turn-1", "user-1", "run-1", startedAt))
	if err != nil {
		t.Fatalf("begin first turn: %v", err)
	}
	if _, err := store.FinishTurn(context.Background(), sessiondomain.FinishTurnInput{
		SessionID: first.Session.ID, TurnID: first.Turn.ID, RunID: first.Run.ID,
		TurnStatus: sessiondomain.TurnCompleted, RunStatus: sessiondomain.RunCompleted,
		AssistantMessageID: "assistant-1", AssistantContent: "done", FinishedAt: startedAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("finish first turn: %v", err)
	}

	const count = 12
	results := make(chan sessiondomain.BeginTurnResult, count)
	errorsChannel := make(chan error, count)
	var waitGroup sync.WaitGroup
	for index := 0; index < count; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			result, err := store.BeginTurn(context.Background(), sessiondomain.BeginTurnInput{
				SessionID: first.Session.ID,
				TurnID:    sessiondomain.TurnID(fmt.Sprintf("turn-%02d", index+2)), UserMessageID: sessiondomain.MessageID(fmt.Sprintf("user-%02d", index+2)), RunID: sessiondomain.RunID(fmt.Sprintf("run-%02d", index+2)),
				Objective: "Concurrent task", UserContent: "Concurrent task", Provider: "openai", Model: "model", APIMode: "responses", Dialect: "openai",
				BudgetJSON: json.RawMessage(`{"max_steps":10}`), StartedAt: startedAt.Add(2 * time.Second),
			})
			if err != nil {
				errorsChannel <- err
				return
			}
			results <- result
		}(index)
	}
	waitGroup.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		t.Fatalf("concurrent BeginTurn failed: %v", err)
	}
	var turnSequences, messageSequences []int
	for result := range results {
		turnSequences = append(turnSequences, int(result.Turn.Sequence))
		messageSequences = append(messageSequences, int(result.Message.Sequence))
	}
	sort.Ints(turnSequences)
	sort.Ints(messageSequences)
	for index := 0; index < count; index++ {
		if turnSequences[index] != index+2 || messageSequences[index] != index+3 {
			t.Fatalf("non-atomic sequences: turns=%v messages=%v", turnSequences, messageSequences)
		}
	}
}

func TestSQLiteStoreFinishFailureRollsBackAllRecords(t *testing.T) {
	database, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store, _ := NewStore(database)
	startedAt := time.Unix(100, 0).UTC()
	first, err := store.BeginFirstTurn(context.Background(), sqliteFirstInput(t.TempDir(), "project-1", "session-1", "turn-1", "user-1", "run-1", startedAt))
	if err != nil {
		t.Fatalf("begin first turn: %v", err)
	}
	_, err = store.FinishTurn(context.Background(), sessiondomain.FinishTurnInput{
		SessionID: first.Session.ID, TurnID: first.Turn.ID, RunID: first.Run.ID,
		TurnStatus: sessiondomain.TurnCompleted, RunStatus: sessiondomain.RunCompleted,
		AssistantMessageID: first.Message.ID, AssistantContent: "duplicate ID", FinishedAt: startedAt.Add(time.Second),
	})
	if !errors.Is(err, sessiondomain.ErrConflict) {
		t.Fatalf("duplicate assistant message did not fail transaction: %v", err)
	}
	run, err := store.GetRun(context.Background(), first.Run.ID)
	if err != nil || run.Status != sessiondomain.RunRunning || run.FinishedAt != nil {
		t.Fatalf("failed completion changed run: run=%#v err=%v", run, err)
	}
	messages, err := store.ListMessages(context.Background(), first.Session.ID)
	if err != nil || len(messages) != 1 || messages[0].Role != sessiondomain.MessageUser {
		t.Fatalf("failed completion changed conversation: messages=%#v err=%v", messages, err)
	}
}

func sqliteFirstInput(projectRoot string, projectID sessiondomain.ProjectID, sessionID sessiondomain.ConversationSessionID, turnID sessiondomain.TurnID, messageID sessiondomain.MessageID, runID sessiondomain.RunID, startedAt time.Time) sessiondomain.BeginFirstTurnInput {
	return sessiondomain.BeginFirstTurnInput{
		ProjectID: projectID, CanonicalPath: filepath.Clean(projectRoot), ProjectName: "Project",
		SessionID: sessionID, SessionTitle: "Fix tests", TurnID: turnID, UserMessageID: messageID,
		RunID: runID, Objective: "Fix tests", UserContent: "Fix tests", Provider: "openai", Model: "model",
		APIMode: "responses", Dialect: "openai", BudgetJSON: json.RawMessage(`{"max_steps":10}`), StartedAt: startedAt,
	}
}
