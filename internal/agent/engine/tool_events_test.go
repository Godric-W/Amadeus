package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type failingEventSink struct{ err error }

func (sink failingEventSink) Publish(context.Context, protocol.Event) error { return sink.err }

func TestToolEventObserverPersistsPresentationOnCompletedItem(t *testing.T) {
	sink := protocol.NewMemorySink()
	var appended []rollout.RolloutItem
	observer := NewToolEventObserver(func(_ context.Context, _ protocol.TurnID, items ...rollout.RolloutItem) error {
		appended = append(appended, items...)
		return nil
	}, "thread-1", protocol.TurnID("turn-1"), sink, nil)
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

func TestToolEventObserverPersistsImagePayloadExactlyOnce(t *testing.T) {
	sink := protocol.NewMemorySink()
	var appended []rollout.RolloutItem
	observer := NewToolEventObserver(func(_ context.Context, _ protocol.TurnID, items ...rollout.RolloutItem) error {
		appended = append(appended, items...)
		return nil
	}, "thread-1", protocol.TurnID("turn-1"), sink, nil)
	call := tool.NewCall("image-call", "view_image", []byte(`{"path":"image.png"}`))
	if err := observer.ToolCallStarted(context.Background(), tool.ToolSpec{Name: "view_image", SideEffect: tool.SideEffectRead}, call); err != nil {
		t.Fatal(err)
	}
	if err := observer.ToolCallCompleted(context.Background(), tool.ToolExecution{
		Call: call,
		Output: tool.ToolResult{
			ToolName: "view_image", Text: "Viewed image.png.",
			Parts:    []tool.ContentPart{{Kind: tool.ContentImage, MediaType: "image/png", Data: "dW5pcXVlLWltYWdlLXBheWxvYWQ=", Detail: "high"}},
			Metadata: map[string]any{"path": "image.png", "prepared_width": 32, "prepared_height": 32},
		},
		Outcome: tool.ToolCallOutcome{Status: tool.ToolCallCompleted},
	}); err != nil {
		t.Fatal(err)
	}
	if len(appended) != 2 {
		t.Fatalf("appended items = %d, want 2", len(appended))
	}
	response := appended[0].(rollout.ResponseItem)
	if len(response.Parts) != 1 || response.Parts[0].Data == "" || response.Result == nil || response.Result.Parts[0].Data != "" {
		t.Fatalf("canonical response did not isolate image payload: %#v", response)
	}
	completed := appended[1].(rollout.EventMsgItem).Msg.(protocol.ItemCompletedEvent)
	if completed.Item.ToolResult == nil || completed.Item.ToolResult.Parts[0].Data != "" {
		t.Fatalf("completed rollout retained image payload: %#v", completed.Item.ToolResult)
	}
	encoded, err := json.Marshal(appended)
	if err != nil {
		t.Fatal(err)
	}
	if count := bytes.Count(encoded, []byte("dW5pcXVlLWltYWdlLXBheWxvYWQ=")); count != 1 {
		t.Fatalf("image payload persisted %d times: %s", count, encoded)
	}
	events := sink.Snapshot()
	liveCompleted := events[len(events)-1].Msg.(protocol.ItemCompletedEvent)
	if liveCompleted.Item.ToolResult == nil || liveCompleted.Item.ToolResult.Parts[0].Data != "" {
		t.Fatalf("live completion retained image payload: %#v", liveCompleted.Item.ToolResult)
	}
}

func TestToolEventObserverPersistsOnlyResultForUpdatePlan(t *testing.T) {
	sink := protocol.NewMemorySink()
	var appended []rollout.RolloutItem
	observer := NewToolEventObserver(func(_ context.Context, _ protocol.TurnID, items ...rollout.RolloutItem) error {
		appended = append(appended, items...)
		return nil
	}, "thread-1", protocol.TurnID("turn-1"), sink, nil)
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

func TestToolEventObserverPersistsTypedCollaborationItem(t *testing.T) {
	sink := protocol.NewMemorySink()
	var appended []rollout.RolloutItem
	observer := NewToolEventObserver(func(_ context.Context, _ protocol.TurnID, items ...rollout.RolloutItem) error {
		appended = append(appended, items...)
		return nil
	}, "root-1", "turn-1", sink, nil)
	call := tool.NewCall("call-agent", "spawn_agent", []byte(`{"message":"inspect session ownership"}`))
	if err := observer.ToolCallStarted(context.Background(), tool.ToolSpec{Name: "spawn_agent"}, call); err != nil {
		t.Fatal(err)
	}
	if err := observer.ToolCallCompleted(context.Background(), tool.ToolExecution{
		Call:    call,
		Output:  tool.ToolResult{ToolName: "spawn_agent", Text: `{"agent_id":"child-1","nickname":"atlas"}`, Data: map[string]any{"agent_id": "child-1", "nickname": "atlas"}},
		Outcome: tool.ToolCallOutcome{Status: tool.ToolCallCompleted},
	}); err != nil {
		t.Fatal(err)
	}
	if len(appended) != 2 {
		t.Fatalf("appended = %#v", appended)
	}
	completed := appended[1].(rollout.EventMsgItem).Msg.(protocol.ItemCompletedEvent).Item
	if completed.Kind != protocol.ItemCollabAgentToolCall || completed.CollabAgent == nil {
		t.Fatalf("completed collaboration item = %#v", completed)
	}
	if completed.CollabAgent.Tool != protocol.CollabAgentSpawnAgent || completed.CollabAgent.SenderThreadID != "root-1" || completed.CollabAgent.Prompt != "inspect session ownership" {
		t.Fatalf("collaboration payload = %#v", completed.CollabAgent)
	}
	if len(completed.CollabAgent.ReceiverAgents) != 1 || completed.CollabAgent.ReceiverAgents[0].AgentNickname != "atlas" {
		t.Fatalf("collaboration receivers = %#v", completed.CollabAgent.ReceiverAgents)
	}
}

func TestToolEventObserverRollsBackCollaborationStartOnPublishFailure(t *testing.T) {
	observer := NewToolEventObserver(func(context.Context, protocol.TurnID, ...rollout.RolloutItem) error {
		return nil
	}, "root-1", "turn-1", failingEventSink{err: errors.New("publish failed")}, nil).(*toolEventObserver)
	call := tool.NewCall("call-agent", "spawn_agent", []byte(`{"message":"inspect"}`))
	if err := observer.ToolCallStarted(context.Background(), tool.ToolSpec{Name: "spawn_agent"}, call); err == nil {
		t.Fatal("expected publish failure")
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.collaboration) != 0 {
		t.Fatalf("collaboration presentations leaked: %#v", observer.collaboration)
	}
}

func TestCompleteCloseAgentItemPreservesPreviousStatus(t *testing.T) {
	now := time.Now().UTC()
	item := protocol.CollabAgentToolCallItem{
		ID: "call-close", Tool: protocol.CollabAgentCloseAgent, Status: protocol.CollabAgentToolInProgress,
		SenderThreadID: "root", ReceiverAgents: []protocol.CollabAgentRef{{ThreadID: "child-1", AgentNickname: "atlas"}}, CreatedAt: now,
	}
	execution := tool.ToolExecution{
		Call: tool.NewCall("call-close", "close_agent", []byte(`{"id":"child-1"}`)),
		Output: tool.ToolResult{Data: map[string]any{
			"agent_id": "child-1", "nickname": "atlas",
			"previous_status": protocol.AgentStatus{Kind: protocol.AgentStatusCompleted, Message: "done"},
		}},
		Outcome: tool.ToolCallOutcome{Status: tool.ToolCallCompleted},
	}
	completed := completeCollabAgentItem(item, execution, now.Add(time.Second), protocol.ItemStatusCompleted)
	state, exists := completed.AgentsStates["child-1"]
	if !exists || state.Status.Kind != protocol.AgentStatusCompleted || state.Status.Message != "done" {
		t.Fatalf("close state = %#v", completed.AgentsStates)
	}
}
