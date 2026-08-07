package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestMemoryStoreCanonicalRolloutLifecycle(t *testing.T) {
	store := NewMemoryStore()
	startedAt := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	first, err := store.BeginFirstRun(context.Background(), firstRunInput(t.TempDir(), "project-1", "session-1", "item-1", "run-1", startedAt))
	if err != nil {
		t.Fatalf("begin first run: %v", err)
	}
	if first.Session.NextRunSequence != 2 || first.Session.NextItemSequence != 2 || first.Item.Sequence != 1 {
		t.Fatalf("unexpected first records: %#v", first)
	}
	assistant, _ := EncodePayload(AssistantMessagePayload{Content: "done"})
	finished, err := store.FinishRun(context.Background(), FinishRunInput{
		SessionID: first.Session.ID, RunID: first.Run.ID, RunStatus: RunCompleted,
		UsageJSON: json.RawMessage(`{"input_tokens":10}`), FinishedAt: startedAt.Add(time.Second),
		TerminalItems: []AppendItem{{ID: "item-2", RunID: first.Run.ID, Kind: RolloutAssistantMessage, Payload: assistant, CreatedAt: startedAt.Add(time.Second)}},
	})
	if err != nil {
		t.Fatalf("finish run: %v", err)
	}
	if finished.Run.Status != RunCompleted || len(finished.Items) != 1 || finished.Items[0].Sequence != 2 {
		t.Fatalf("unexpected finish result: %#v", finished)
	}
	items, err := store.ListItems(context.Background(), first.Session.ID)
	if err != nil || len(items) != 2 {
		t.Fatalf("list items: count=%d err=%v", len(items), err)
	}
}

func TestMemoryStoreRecoversRunningRunWithMarker(t *testing.T) {
	store := NewMemoryStore()
	startedAt := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	first, err := store.BeginFirstRun(context.Background(), firstRunInput(t.TempDir(), "project-1", "session-1", "item-1", "run-1", startedAt))
	if err != nil {
		t.Fatalf("begin first run: %v", err)
	}
	if err := store.RecoverRunningRuns(context.Background(), first.Session.ID, startedAt.Add(time.Second)); err != nil {
		t.Fatalf("recover running run: %v", err)
	}
	run, _ := store.GetRun(context.Background(), first.Run.ID)
	items, _ := store.ListItems(context.Background(), first.Session.ID)
	if run.Status != RunInterrupted || len(items) != 2 || items[1].Kind != RolloutRunInterrupted {
		t.Fatalf("unexpected recovery: run=%#v items=%#v", run, items)
	}
}

func TestMemoryStoreRepairsPendingToolCallBeforeRecoveryMarker(t *testing.T) {
	store := NewMemoryStore()
	startedAt := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	first, err := store.BeginFirstRun(context.Background(), firstRunInput(t.TempDir(), "project-1", "session-1", "item-1", "run-1", startedAt))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := EncodePayload(ToolCallPayload{Calls: []ToolCallRecord{{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"README.md"}`)}}})
	if _, err := store.AppendItems(context.Background(), AppendItemsInput{SessionID: first.Session.ID, Items: []AppendItem{{
		ID: "item-call", RunID: first.Run.ID, Kind: RolloutToolCall, Payload: payload, CreatedAt: startedAt.Add(time.Second),
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverRunningRuns(context.Background(), first.Session.ID, startedAt.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	items, _ := store.ListItems(context.Background(), first.Session.ID)
	if len(items) != 4 || items[2].Kind != RolloutToolResult || items[3].Kind != RolloutRunInterrupted {
		t.Fatalf("unexpected repaired rollout: %#v", items)
	}
	result, err := DecodeToolResult(items[2])
	if err != nil || result.CallID != "call-1" || result.Status != "interrupted" || result.Error == nil || result.Error.Kind != "process_terminated" {
		t.Fatalf("unexpected synthetic result: payload=%#v err=%v", result, err)
	}
	marker, err := DecodeRunMarker(items[3])
	if err != nil || len(marker.ActiveCalls) != 1 || marker.ActiveCalls[0] != "call-1" {
		t.Fatalf("unexpected recovery marker: payload=%#v err=%v", marker, err)
	}
}

func firstRunInput(projectPath string, projectID ProjectID, sessionID SessionID, itemID RolloutItemID, runID RunID, startedAt time.Time) BeginFirstRunInput {
	return BeginFirstRunInput{
		ProjectID: projectID, CanonicalPath: projectPath, ProjectName: "project", SessionID: sessionID, SessionTitle: "Fix tests",
		RunID: runID, UserItemID: itemID, UserContent: "Fix tests", Mode: RunModeExecute, StartedAt: startedAt,
	}
}
