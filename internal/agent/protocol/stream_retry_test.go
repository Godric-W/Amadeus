package protocol

import (
	"testing"
	"time"
)

func TestTranscriptStateRetryDeltaResetReplacesAttemptDraft(t *testing.T) {
	state := NewTranscriptState("thread-1")
	createdAt := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	events := []Event{
		scopedTestEvent(ItemStartedEvent{Item: TurnItem{ID: "assistant-1", Kind: ItemAssistantMessage, Status: ItemInProgress, CreatedAt: createdAt}}),
		scopedTestEvent(AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "partial"}),
		scopedTestEvent(AgentMessageContentDeltaEvent{ItemID: "assistant-1", Reset: true}),
		scopedTestEvent(AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "recovered"}),
	}
	for _, event := range events {
		if err := state.Apply(event); err != nil {
			t.Fatalf("apply %T: %v", event.Msg, err)
		}
	}
	if got := state.Active["assistant-1"].Text; got != "recovered" {
		t.Fatalf("retry draft = %q, want recovered", got)
	}
}

func TestTranscriptStateTransientStreamErrorIsNotTerminal(t *testing.T) {
	state := NewTranscriptState("thread-1")
	if err := state.Apply(scopedTestEvent(StreamErrorEvent{Message: "Reconnecting... 1/5", WillRetry: true})); err != nil {
		t.Fatal(err)
	}
	if state.Error != "" {
		t.Fatalf("transient stream error became transcript error: %q", state.Error)
	}
	if err := state.Apply(scopedTestEvent(StreamErrorEvent{Message: "provider network error"})); err != nil {
		t.Fatal(err)
	}
	if state.Error != "provider network error" {
		t.Fatalf("terminal stream error = %q", state.Error)
	}
}
