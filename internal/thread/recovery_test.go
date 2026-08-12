package thread

import (
	"encoding/json"
	"testing"

	"github.com/Godric-W/Amadeus/internal/rollout"
)

func TestIncompleteTurnFindsLatestStillOpenTurn(t *testing.T) {
	started, err := rollout.NewItem(rollout.KindTurnStarted, rollout.TurnStarted{Input: "work"})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := rollout.NewItem(rollout.KindTurnCompleted, rollout.TurnCompleted{Status: rollout.TurnStatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	lines := []rollout.Line{
		{TurnID: "turn-1", Item: started},
		{TurnID: "turn-2", Item: started},
		{TurnID: "turn-2", Item: completed},
	}
	if got := incompleteTurn(lines); got != "turn-1" {
		t.Fatalf("incomplete turn = %q, want turn-1", got)
	}
}

func TestPendingToolCallsDeduplicatesCallIDs(t *testing.T) {
	call, err := rollout.NewRawItem(rollout.KindResponseItem, json.RawMessage(`{"type":"tool_call","call_id":"call-1","name":"read"}`))
	if err != nil {
		t.Fatal(err)
	}
	lines := []rollout.Line{{TurnID: "turn-1", Item: call}, {TurnID: "turn-1", Item: call}}
	pending := pendingToolCalls(lines, "turn-1")
	if len(pending) != 1 || pending[0].ID != "call-1" {
		t.Fatalf("pending calls = %#v", pending)
	}
}
