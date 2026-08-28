package threadmanager

import (
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/testutil"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func TestPersistedAgentLifecycleUsesCanonicalTerminalReducer(t *testing.T) {
	final := "verified result"
	tests := []struct {
		name    string
		events  []protocol.EventMsg
		status  protocol.AgentStatusKind
		outcome protocol.TurnOutcome
		message string
	}{
		{name: "session meta only", status: protocol.AgentStatusPendingInit},
		{name: "open turn", events: []protocol.EventMsg{protocol.TurnStartedEvent{TurnID: "turn-1"}}, status: protocol.AgentStatusInterrupted, outcome: protocol.TurnOutcomeAborted},
		{name: "completed", events: []protocol.EventMsg{
			protocol.TurnStartedEvent{TurnID: "turn-1"},
			protocol.TurnCompleteEvent{TurnID: "turn-1", Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, LastAgentMessage: &final},
		}, status: protocol.AgentStatusCompleted, outcome: protocol.TurnOutcomeCompleted, message: final},
		{name: "blocked", events: []protocol.EventMsg{
			protocol.TurnStartedEvent{TurnID: "turn-1"},
			protocol.TurnCompleteEvent{TurnID: "turn-1", Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeBlocked, Reason: "budget reached"},
		}, status: protocol.AgentStatusCompleted, outcome: protocol.TurnOutcomeBlocked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lines := make([]rollout.Line, 0, len(test.events))
			for index, event := range test.events {
				lines = append(lines, rollout.Line{Sequence: uint64(index + 1), Item: rollout.EventMsgItem{Msg: event}})
			}
			state := persistedAgentLifecycle(threadstore.InitialHistory{Kind: threadstore.InitialHistoryResumed, Lines: lines})
			if state.Status.Kind != test.status || state.Status.Message != test.message {
				t.Fatalf("status = %#v", state.Status)
			}
			if test.outcome == "" {
				if state.LastTurn != nil {
					t.Fatalf("unexpected last turn = %#v", state.LastTurn)
				}
			} else if state.LastTurn == nil || state.LastTurn.Outcome != test.outcome {
				t.Fatalf("last turn = %#v, want %q", state.LastTurn, test.outcome)
			}
		})
	}
}

func TestOpenChildIDsUsesLatestCanonicalEdgeState(t *testing.T) {
	rootID, firstID, secondID := testutil.ThreadID(1), testutil.ThreadID(2), testutil.ThreadID(3)
	now := time.Now().UTC()
	lines := []rollout.Line{
		{Item: rollout.AgentSpawnEdgeItem{AgentID: firstID, ParentThreadID: rootID, State: protocol.AgentSpawnEdgeOpen, UpdatedAt: now}},
		{Item: rollout.AgentSpawnEdgeItem{AgentID: secondID, ParentThreadID: rootID, State: protocol.AgentSpawnEdgeOpen, UpdatedAt: now}},
		{Item: rollout.AgentSpawnEdgeItem{AgentID: firstID, ParentThreadID: rootID, State: protocol.AgentSpawnEdgeClosed, UpdatedAt: now.Add(time.Second)}},
	}
	open, err := openChildIDs(rootID, lines)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0] != secondID {
		t.Fatalf("open child IDs = %v", open)
	}
}
