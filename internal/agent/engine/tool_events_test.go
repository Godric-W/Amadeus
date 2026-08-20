package engine

import (
	"context"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestToolEventObserverPersistsPresentationOnCompletedItem(t *testing.T) {
	sink := protocol.NewMemorySink()
	var appended []rollout.RolloutItem
	observer := NewToolEventObserver(func(_ context.Context, _ protocol.TurnID, items ...rollout.RolloutItem) error {
		appended = append(appended, items...)
		return nil
	}, protocol.TurnID("turn-1"), sink)
	call := tool.NewCall("call-1", "grep", []byte(`{"query":"Approval","path":"internal"}`))
	if err := observer.ToolCallStarted(context.Background(), tool.ToolSpec{Name: "grep", SideEffect: tool.SideEffectRead}, call); err != nil {
		t.Fatal(err)
	}
	if err := observer.ToolCallCompleted(context.Background(), tool.ToolExecution{
		Call:    call,
		Output:  tool.ToolResult{ToolName: "grep", Text: "match"},
		Outcome: tool.ToolCallOutcome{Status: tool.ToolCallCompleted, Duration: 15 * time.Millisecond},
	}); err != nil {
		t.Fatal(err)
	}
	if len(appended) != 2 {
		t.Fatalf("appended items = %d, want 2", len(appended))
	}
	if _, ok := appended[0].(rollout.ResponseItem); !ok {
		t.Fatalf("first appended item = %T, want rollout.ResponseItem", appended[0])
	}
	completedRollout, ok := appended[1].(rollout.EventMsgItem)
	if !ok {
		t.Fatalf("second appended item = %T, want rollout.EventMsgItem", appended[1])
	}
	if _, ok := completedRollout.Msg.(protocol.ItemCompletedEvent); !ok {
		t.Fatalf("persisted event = %T, want protocol.ItemCompletedEvent", completedRollout.Msg)
	}
	events := sink.Snapshot()
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	completed, ok := events[1].Msg.(protocol.ItemCompletedEvent)
	if !ok {
		t.Fatalf("completed event = %T", events[1].Msg)
	}
	payload, ok := completed.Item.Payload.(map[string]any)
	if !ok {
		t.Fatalf("completed payload = %T", completed.Item.Payload)
	}
	if payload["action_summary"] != "Search Approval in internal" || payload["side_effect"] != "read" || payload["duration"] != "15ms" {
		t.Fatalf("completed presentation payload = %#v", payload)
	}
}

func TestToolEventObserverPersistsOnlyResultForUpdatePlan(t *testing.T) {
	sink := protocol.NewMemorySink()
	var appended []rollout.RolloutItem
	observer := NewToolEventObserver(func(_ context.Context, _ protocol.TurnID, items ...rollout.RolloutItem) error {
		appended = append(appended, items...)
		return nil
	}, protocol.TurnID("turn-1"), sink)
	call := tool.NewCall("call-1", "update_plan", []byte(`{"plan":[]}`))
	if err := observer.ToolCallStarted(context.Background(), tool.ToolSpec{Name: "update_plan"}, call); err != nil {
		t.Fatal(err)
	}
	if err := observer.ToolCallCompleted(context.Background(), tool.ToolExecution{
		Call: call, Output: tool.ToolResult{ToolName: "update_plan", Text: "Plan updated"},
		Outcome: tool.ToolCallOutcome{Status: tool.ToolCallCompleted},
	}); err != nil {
		t.Fatal(err)
	}
	if len(appended) != 1 {
		t.Fatalf("appended items = %d, want 1", len(appended))
	}
	if item, ok := appended[0].(rollout.ResponseItem); !ok || item.Type != rollout.ResponseToolResult {
		t.Fatalf("appended item = %#v, want tool result", appended[0])
	}
	if events := sink.Snapshot(); len(events) != 0 {
		t.Fatalf("events = %#v, want none", events)
	}
}
