package protocol

import (
	"testing"
	"time"
)

func TestTranscriptStateRetryDeltaResetReplacesAttemptDraft(t *testing.T) {
	state := NewTranscriptState("thread-1")
	createdAt := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	events := []SessionEvent{
		{ThreadID: "thread-1", TurnID: "turn-1", Message: ItemStarted{Item: TurnItem{ID: "assistant-1", Kind: ItemAssistantMessage, Status: ItemInProgress, CreatedAt: createdAt}}},
		{ThreadID: "thread-1", TurnID: "turn-1", Message: AssistantMessageDelta{ItemID: "assistant-1", Delta: "partial"}},
		{ThreadID: "thread-1", TurnID: "turn-1", Message: AssistantMessageDelta{ItemID: "assistant-1", Reset: true}},
		{ThreadID: "thread-1", TurnID: "turn-1", Message: AssistantMessageDelta{ItemID: "assistant-1", Delta: "recovered"}},
	}
	for _, event := range events {
		if err := state.Apply(event); err != nil {
			t.Fatalf("apply %T: %v", event.Message, err)
		}
	}
	if got := state.Active["assistant-1"].Text; got != "recovered" {
		t.Fatalf("retry draft = %q, want recovered", got)
	}
}

func TestTranscriptStateTransientStreamErrorIsNotTerminal(t *testing.T) {
	state := NewTranscriptState("thread-1")
	if err := state.Apply(SessionEvent{ThreadID: "thread-1", TurnID: "turn-1", Message: StreamError{Message: "Reconnecting... 1/5", WillRetry: true}}); err != nil {
		t.Fatal(err)
	}
	if state.Error != "" {
		t.Fatalf("transient stream error became transcript error: %q", state.Error)
	}
	if err := state.Apply(SessionEvent{ThreadID: "thread-1", TurnID: "turn-1", Message: StreamError{Message: "provider network error"}}); err != nil {
		t.Fatal(err)
	}
	if state.Error != "provider network error" {
		t.Fatalf("terminal stream error = %q", state.Error)
	}
}
