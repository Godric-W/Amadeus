package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
)

func TestStorePersistsCanonicalRunAndRollout(t *testing.T) {
	store, database := newSQLiteStore(t)
	defer database.Close()
	startedAt := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	first, err := store.BeginFirstRun(context.Background(), sqliteFirstRunInput(t.TempDir(), "project-1", "session-1", "item-1", "run-1", startedAt))
	if err != nil {
		t.Fatalf("begin first run: %v", err)
	}
	if first.Session.NextRunSequence != 2 || first.Session.NextItemSequence != 2 || first.Item.Sequence != 1 {
		t.Fatalf("unexpected first records: %#v", first)
	}
	second, err := store.BeginRun(context.Background(), sessiondomain.BeginRunInput{
		SessionID: first.Session.ID, RunID: "run-2", UserItemID: "item-2", UserContent: "continue",
		Provider: "openai", Model: "model", APIMode: "responses", Dialect: "openai", Mode: sessiondomain.RunModePlan,
		StartedAt: startedAt.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("begin second run: %v", err)
	}
	if second.Run.Sequence != 2 || second.Item.Sequence != 2 || second.Run.Mode != sessiondomain.RunModePlan {
		t.Fatalf("unexpected second records: %#v", second)
	}
	assistant, _ := sessiondomain.EncodePayload(sessiondomain.AssistantMessagePayload{Content: "done"})
	finished, err := store.FinishRun(context.Background(), sessiondomain.FinishRunInput{
		SessionID: second.Session.ID, RunID: second.Run.ID, RunStatus: sessiondomain.RunCompleted,
		UsageJSON: json.RawMessage(`{"input_tokens":4}`), FinishedAt: startedAt.Add(2 * time.Second),
		TerminalItems: []sessiondomain.AppendItem{{ID: "item-3", RunID: second.Run.ID, Kind: sessiondomain.RolloutAssistantMessage, Payload: assistant, CreatedAt: startedAt.Add(2 * time.Second)}},
	})
	if err != nil {
		t.Fatalf("finish second run: %v", err)
	}
	if finished.Run.Status != sessiondomain.RunCompleted || len(finished.Items) != 1 || finished.Items[0].Sequence != 3 {
		t.Fatalf("unexpected finish result: %#v", finished)
	}
	items, err := store.ListItems(context.Background(), first.Session.ID)
	if err != nil || len(items) != 3 {
		t.Fatalf("list rollout: count=%d err=%v", len(items), err)
	}
}

func TestStoreRecoversRunningRunsAsInterruptedMarkers(t *testing.T) {
	store, database := newSQLiteStore(t)
	defer database.Close()
	startedAt := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	first, err := store.BeginFirstRun(context.Background(), sqliteFirstRunInput(t.TempDir(), "project-1", "session-1", "item-1", "run-1", startedAt))
	if err != nil {
		t.Fatalf("begin run: %v", err)
	}
	if err := store.RecoverRunningRuns(context.Background(), first.Session.ID, startedAt.Add(time.Second)); err != nil {
		t.Fatalf("recover running runs: %v", err)
	}
	run, _ := store.GetRun(context.Background(), first.Run.ID)
	items, _ := store.ListItems(context.Background(), first.Session.ID)
	if run.Status != sessiondomain.RunInterrupted || len(items) != 2 || items[1].Kind != sessiondomain.RolloutRunInterrupted {
		t.Fatalf("unexpected recovery: run=%#v items=%#v", run, items)
	}
}

func TestStoreRepairsPendingToolCallBeforeRecoveryMarker(t *testing.T) {
	store, database := newSQLiteStore(t)
	defer database.Close()
	startedAt := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	first, err := store.BeginFirstRun(context.Background(), sqliteFirstRunInput(t.TempDir(), "project-1", "session-1", "item-1", "run-1", startedAt))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := sessiondomain.EncodePayload(sessiondomain.ToolCallPayload{Calls: []sessiondomain.ToolCallRecord{{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"README.md"}`)}}})
	if _, err := store.AppendItems(context.Background(), sessiondomain.AppendItemsInput{SessionID: first.Session.ID, Items: []sessiondomain.AppendItem{{
		ID: "item-call", RunID: first.Run.ID, Kind: sessiondomain.RolloutToolCall, Payload: payload, CreatedAt: startedAt.Add(time.Second),
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverRunningRuns(context.Background(), first.Session.ID, startedAt.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListItems(context.Background(), first.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 || items[2].Kind != sessiondomain.RolloutToolResult || items[3].Kind != sessiondomain.RolloutRunInterrupted {
		t.Fatalf("unexpected repaired rollout: %#v", items)
	}
	result, err := sessiondomain.DecodeToolResult(items[2])
	if err != nil || result.CallID != "call-1" || result.Status != "interrupted" || result.Error == nil || result.Error.Kind != "process_terminated" {
		t.Fatalf("unexpected synthetic result: payload=%#v err=%v", result, err)
	}
}

func newSQLiteStore(t *testing.T) (*Store, *Database) {
	t.Helper()
	database, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	store, err := NewStore(database)
	if err != nil {
		database.Close()
		t.Fatalf("new store: %v", err)
	}
	return store, database
}

func sqliteFirstRunInput(projectPath string, projectID sessiondomain.ProjectID, sessionID sessiondomain.SessionID, itemID sessiondomain.RolloutItemID, runID sessiondomain.RunID, startedAt time.Time) sessiondomain.BeginFirstRunInput {
	return sessiondomain.BeginFirstRunInput{
		ProjectID: projectID, CanonicalPath: projectPath, ProjectName: "project", SessionID: sessionID, SessionTitle: "Fix tests",
		RunID: runID, UserItemID: itemID, UserContent: "Fix tests", Mode: sessiondomain.RunModeExecute, StartedAt: startedAt,
	}
}

func TestSQLiteStoreRenamesAndCascadesSessionDelete(t *testing.T) {
	store, database := newSQLiteStore(t)
	defer database.Close()
	startedAt := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	started, err := store.BeginFirstRun(context.Background(), sqliteFirstRunInput(t.TempDir(), "project-rename", "session-rename", "item-rename", "run-rename", startedAt))
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := sessiondomain.EncodePayload(sessiondomain.AssistantMessagePayload{Content: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishRun(context.Background(), sessiondomain.FinishRunInput{
		SessionID: started.Session.ID, RunID: started.Run.ID, RunStatus: sessiondomain.RunCompleted, FinishedAt: startedAt.Add(time.Second),
		TerminalItems: []sessiondomain.AppendItem{{ID: "item-finished", RunID: started.Run.ID, Kind: sessiondomain.RolloutAssistantMessage, Payload: assistant, CreatedAt: startedAt.Add(time.Second)}},
	}); err != nil {
		t.Fatal(err)
	}
	renamed, err := store.RenameSession(context.Background(), sessiondomain.RenameSessionInput{SessionID: started.Session.ID, Title: "Renamed Session", UpdatedAt: startedAt.Add(2 * time.Second)})
	if err != nil || renamed.Title != "Renamed Session" {
		t.Fatalf("rename Session: value=%#v err=%v", renamed, err)
	}
	if err := store.DeleteSession(context.Background(), started.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSession(context.Background(), started.Session.ID); !errors.Is(err, sessiondomain.ErrNotFound) {
		t.Fatalf("deleted Session lookup error = %v", err)
	}
	if _, err := store.GetRun(context.Background(), started.Run.ID); !errors.Is(err, sessiondomain.ErrNotFound) {
		t.Fatalf("cascaded Run lookup error = %v", err)
	}
	items, err := store.ListItems(context.Background(), started.Session.ID)
	if err != nil || len(items) != 0 {
		t.Fatalf("cascaded rollout items = %#v, err=%v", items, err)
	}
}
