package transcript

import (
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

func TestStateLiveItemLifecycle(t *testing.T) {
	state := New(testutil.ThreadID(1))
	created := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	started := protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: created}
	apply(t, state, protocol.TurnStartedEvent{})
	apply(t, state, protocol.ItemStartedEvent{Item: started})
	apply(t, state, protocol.AgentMessageContentDeltaEvent{ItemID: started.ID, Delta: "hello"})
	finished := started
	finished.Status = protocol.ItemStatusCompleted
	finished.CompletedAt = created.Add(time.Second)
	apply(t, state, protocol.ItemCompletedEvent{Item: finished})
	if !state.Working || len(state.Active) != 0 || len(state.Items) != 1 || state.Items[0].Text != "" {
		t.Fatalf("unexpected state after completion: %#v", state)
	}
	apply(t, state, protocol.TurnCompleteEvent{})
	if state.Working {
		t.Fatal("turn completion did not clear working state")
	}
}

func TestStateReplayAndRepeatedCompletionAreIdempotent(t *testing.T) {
	state := New(testutil.ThreadID(1))
	when := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	item := protocol.TurnItem{ID: "tool-1", Kind: protocol.ItemToolCall, Status: protocol.ItemStatusCompleted, CreatedAt: when, CompletedAt: when.Add(time.Second), Text: "first"}
	apply(t, state, protocol.ItemCompletedEvent{Item: item})
	item.Text = "second"
	apply(t, state, protocol.ItemCompletedEvent{Item: item})
	if len(state.Items) != 1 || state.Items[0].Text != "second" {
		t.Fatalf("completion replay was not idempotent: %#v", state.Items)
	}
}

func TestStateLateAndUnknownDeltaBehavior(t *testing.T) {
	state := New(testutil.ThreadID(1))
	when := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	item := protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemStatusCompleted, CreatedAt: when, CompletedAt: when.Add(time.Second), Text: "final"}
	apply(t, state, protocol.ItemCompletedEvent{Item: item})
	apply(t, state, protocol.AgentMessageContentDeltaEvent{ItemID: item.ID, Delta: " late"})
	if state.Items[0].Text != "final" {
		t.Fatalf("late delta mutated completed item: %#v", state.Items)
	}
	err := state.Apply(scoped(protocol.AgentMessageContentDeltaEvent{ItemID: "missing", Delta: "x"}))
	if err == nil || !strings.Contains(err.Error(), "unknown item") {
		t.Fatalf("unknown delta error = %v", err)
	}
}

func TestStateRetryAndStreamErrorProjection(t *testing.T) {
	state := New(testutil.ThreadID(1))
	created := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	for _, message := range []protocol.EventMsg{
		protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: created}},
		protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "partial"},
		protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Reset: true},
		protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "recovered"},
		protocol.StreamErrorEvent{Message: "Reconnecting... 1/5", WillRetry: true},
	} {
		apply(t, state, message)
	}
	if state.Active["assistant-1"].Text != "recovered" || state.Error != "" {
		t.Fatalf("retry projection = %#v", state)
	}
	apply(t, state, protocol.StreamErrorEvent{Message: "provider network error"})
	if state.Error != "provider network error" {
		t.Fatalf("terminal stream error = %q", state.Error)
	}
}

func TestStateProjectsContextCompaction(t *testing.T) {
	state := New(testutil.ThreadID(1))
	now := time.Now().UTC()
	apply(t, state, protocol.ItemCompletedEvent{Item: protocol.TurnItem{
		ID: "compact-1", Kind: protocol.ItemContextCompaction, Status: protocol.ItemStatusCompleted,
		CreatedAt: now, CompletedAt: now, Payload: protocol.ContextCompactionItem{Trigger: protocol.CompactionTriggerManual, Reason: protocol.CompactionReasonUserRequested, Phase: protocol.CompactionPhaseStandaloneTurn},
	}})
	if len(state.Items) != 1 || state.Items[0].Kind != protocol.ItemContextCompaction {
		t.Fatalf("compaction projection = %#v", state.Items)
	}
}

func apply(t *testing.T, state *State, message protocol.EventMsg) {
	t.Helper()
	if err := state.Apply(scoped(message)); err != nil {
		t.Fatalf("apply %T: %v", message, err)
	}
}

func scoped(message protocol.EventMsg) protocol.Event {
	return protocol.Event{ID: "submission-1", Msg: protocol.ScopeEventMsg(message, testutil.ThreadID(1), "turn-1")}
}
