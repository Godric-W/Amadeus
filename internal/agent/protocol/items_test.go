package protocol

import (
	"strings"
	"testing"
	"time"
)

func TestEventValidate(t *testing.T) {
	if err := (Event{}).Validate(); err == nil {
		t.Fatal("expected an empty event to be rejected")
	}
	if err := (Event{ID: "submission-1"}).Validate(); err == nil {
		t.Fatal("expected an event without a message to be rejected")
	}
}

func TestApprovalRequestEventValidateTypedPayload(t *testing.T) {
	request := ApprovalRequestEvent{
		RequestID: "request-1",
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Approval:  ApprovalRequest{ID: "request-1", ToolName: "edit"},
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("valid approval request rejected: %v", err)
	}

	request.Approval = ApprovalRequest{}
	if err := request.Validate(); err == nil {
		t.Fatal("expected approval request without payload to be rejected")
	}
}

func TestApprovalDecisionOpCarriesFullDecision(t *testing.T) {
	decision := ApprovalDecisionOp{RequestID: "request-1", OptionID: "allow-session", Outcome: "allow", Scope: "session", Source: "user", Reason: "approved"}
	if decision.RequestID == "" || decision.OptionID != "allow-session" || decision.Outcome != "allow" || decision.Scope != "session" {
		t.Fatalf("unexpected approval decision: %#v", decision)
	}
}

func TestTranscriptStateLiveItemLifecycle(t *testing.T) {
	state := NewTranscriptState("thread-1")
	created := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	started := TurnItem{
		ID: "assistant-1", Kind: ItemAssistantMessage, Status: ItemInProgress, CreatedAt: created,
	}
	if err := state.Apply(scopedTestEvent(TurnStartedEvent{})); err != nil {
		t.Fatalf("apply turn start: %v", err)
	}
	if !state.Working {
		t.Fatal("turn start did not set working state")
	}
	if err := state.Apply(scopedTestEvent(ItemStartedEvent{Item: started})); err != nil {
		t.Fatalf("apply item start: %v", err)
	}
	if err := state.Apply(scopedTestEvent(AgentMessageContentDeltaEvent{ItemID: started.ID, Delta: "hello"})); err != nil {
		t.Fatalf("apply delta: %v", err)
	}
	finished := started
	finished.Status = ItemStatusCompleted
	finished.CompletedAt = created.Add(time.Second)
	if err := state.Apply(scopedTestEvent(ItemCompletedEvent{Item: finished})); err != nil {
		t.Fatalf("apply item completion: %v", err)
	}
	if len(state.Active) != 0 || len(state.Items) != 1 {
		t.Fatalf("unexpected state after completion: active=%d items=%d", len(state.Active), len(state.Items))
	}
	if state.Items[0].Text != "" {
		t.Fatalf("completion should be authoritative, got text %q", state.Items[0].Text)
	}
	if err := state.Apply(scopedTestEvent(TurnCompleteEvent{})); err != nil {
		t.Fatalf("apply turn completion: %v", err)
	}
	if state.Working {
		t.Fatal("turn completion did not clear working state")
	}
}

func TestTranscriptStateReplayCompletedWithoutStart(t *testing.T) {
	state := NewTranscriptState("thread-1")
	completedAt := time.Date(2026, time.August, 13, 0, 0, 1, 0, time.UTC)
	item := TurnItem{
		ID: "tool-1", Kind: ItemToolCall, Status: ItemStatusCompleted,
		CreatedAt: completedAt.Add(-time.Second), CompletedAt: completedAt, Text: "done",
	}
	if err := state.Apply(scopedTestEvent(ItemCompletedEvent{Item: item})); err != nil {
		t.Fatalf("apply replay completion: %v", err)
	}
	if len(state.Items) != 1 || state.Items[0].ID != item.ID {
		t.Fatalf("replay completion was not committed: %#v", state.Items)
	}
}

func TestTranscriptStateRepeatedCompletionIsIdempotent(t *testing.T) {
	state := NewTranscriptState("thread-1")
	when := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	item := TurnItem{ID: "tool-1", Kind: ItemToolCall, Status: ItemStatusCompleted, CreatedAt: when, CompletedAt: when.Add(time.Second), Text: "first"}
	event := scopedTestEvent(ItemCompletedEvent{Item: item})
	if err := state.Apply(event); err != nil {
		t.Fatalf("apply first completion: %v", err)
	}
	item.Text = "second"
	if err := state.Apply(scopedTestEvent(ItemCompletedEvent{Item: item})); err != nil {
		t.Fatalf("apply repeated completion: %v", err)
	}
	if len(state.Items) != 1 || state.Items[0].Text != "second" {
		t.Fatalf("repeated completion was appended instead of replaced: %#v", state.Items)
	}
}

func TestTranscriptStateLateDeltaDoesNotMutateCompletedItem(t *testing.T) {
	state := NewTranscriptState("thread-1")
	when := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	item := TurnItem{ID: "assistant-1", Kind: ItemAssistantMessage, Status: ItemStatusCompleted, CreatedAt: when, CompletedAt: when.Add(time.Second), Text: "final"}
	if err := state.Apply(scopedTestEvent(ItemCompletedEvent{Item: item})); err != nil {
		t.Fatalf("apply completion: %v", err)
	}
	if err := state.Apply(scopedTestEvent(AgentMessageContentDeltaEvent{ItemID: item.ID, Delta: " late"})); err != nil {
		t.Fatalf("late delta should be ignored, got %v", err)
	}
	if state.Items[0].Text != "final" || len(state.Active) != 0 {
		t.Fatalf("late delta mutated completed item: %#v active=%#v", state.Items, state.Active)
	}
}

func TestTranscriptStateUnknownDeltaIsRejected(t *testing.T) {
	state := NewTranscriptState("thread-1")
	err := state.Apply(scopedTestEvent(AgentMessageContentDeltaEvent{ItemID: "missing", Delta: "x"}))
	if err == nil || !strings.Contains(err.Error(), "unknown item") {
		t.Fatalf("expected unknown item error, got %v", err)
	}
}

func TestTranscriptStateContextCompacted(t *testing.T) {
	state := NewTranscriptState("thread-1")
	if err := state.Apply(scopedTestEvent(ContextCompactedEvent{ItemID: "compact-1"})); err != nil {
		t.Fatalf("apply compaction: %v", err)
	}
	if len(state.Items) != 1 || state.Items[0].Kind != ItemContextCompaction || state.Items[0].Text != "" {
		t.Fatalf("unexpected compaction item: %#v", state.Items)
	}
}

func scopedTestEvent(message EventMsg) Event {
	return Event{ID: "submission-1", Msg: ScopeEventMsg(message, "thread-1", "turn-1")}
}
