package thread

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func TestIncompleteTurnFindsLatestStillOpenTurn(t *testing.T) {
	lines := []rollout.Line{
		{Item: rollout.EventMsgItem{Msg: protocol.TurnStartedEvent{ThreadID: "thread-1", TurnID: "turn-1", Input: "work", StartedAt: time.Now().UTC()}}},
		{Item: rollout.EventMsgItem{Msg: protocol.TurnStartedEvent{ThreadID: "thread-1", TurnID: "turn-2", Input: "work", StartedAt: time.Now().UTC()}}},
		{Item: rollout.EventMsgItem{Msg: protocol.TurnCompleteEvent{ThreadID: "thread-1", TurnID: "turn-2", Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, FinishedAt: time.Now().UTC()}}},
	}
	if got := incompleteTurn(lines); got != "turn-1" {
		t.Fatalf("incomplete turn = %q, want turn-1", got)
	}
}

func TestPendingToolCallsDeduplicatesCallIDs(t *testing.T) {
	call, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseToolCall, Role: "assistant", CallID: "call-1", Name: "read", Arguments: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	call = rollout.ScopeItem(call, "thread-1", "turn-1").(rollout.ResponseItem)
	lines := []rollout.Line{{Item: call}, {Item: call}}
	pending := pendingToolCalls(lines, "turn-1")
	if len(pending) != 1 || pending[0].ID != "call-1" {
		t.Fatalf("pending calls = %#v", pending)
	}
}
