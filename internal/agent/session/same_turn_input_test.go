package session

import (
	"errors"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

func TestTurnInputQueueDrainsFIFO(t *testing.T) {
	state := newTurnState()
	queue := InputQueue{}
	for _, content := range []string{"one", "two", "three"} {
		if err := queue.Enqueue(state, UserTurnInput{Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	drained := queue.Drain(state)
	if len(drained) != 3 {
		t.Fatalf("drained = %#v", drained)
	}
	for index, content := range []string{"one", "two", "three"} {
		input, ok := drained[index].(UserTurnInput)
		if !ok || input.Content != content {
			t.Fatalf("drained[%d] = %#v", index, drained[index])
		}
	}
}

func TestTurnInputQueueSealHandshake(t *testing.T) {
	queue := InputQueue{}
	state := newTurnState()
	if err := queue.Enqueue(state, UserTurnInput{Content: "pending"}); err != nil {
		t.Fatal(err)
	}
	if queue.SealIfEmpty(state) {
		t.Fatal("queue sealed while input was pending")
	}
	if got := queue.Drain(state); len(got) != 1 {
		t.Fatalf("drained = %#v", got)
	}
	if !queue.SealIfEmpty(state) {
		t.Fatal("empty queue did not seal")
	}
	if err := queue.Enqueue(state, UserTurnInput{Content: "late"}); !errors.Is(err, errTurnInputQueueSealed) {
		t.Fatalf("enqueue after seal = %v", err)
	}
}

func TestSteerInputRequiresMatchingRegularActiveTurn(t *testing.T) {
	session := newTestSession(nil, nil)
	_, err := session.steerInput(UserTurnInput{Content: "continue"}, "turn-1")
	var steerErr *SteerInputError
	if !errors.As(err, &steerErr) || steerErr.Kind != SteerInputNoActiveTurn {
		t.Fatalf("no-active error = %#v", err)
	}

	session.active = testActiveTurn(TaskKindRegular, "turn-1")
	turnID, err := session.steerInput(UserTurnInput{Content: "continue"}, "turn-1")
	if err != nil || turnID != "turn-1" {
		t.Fatalf("steer = %q, %v", turnID, err)
	}
	if !session.inputQueue.HasPending(session.active.State) {
		t.Fatal("steered input was not queued")
	}

	_, err = session.steerInput(UserTurnInput{Content: "stale"}, "turn-old")
	if !errors.As(err, &steerErr) || steerErr.Kind != SteerInputExpectedTurnMismatch || steerErr.Actual != "turn-1" {
		t.Fatalf("mismatch error = %#v", err)
	}

	session.active = testActiveTurn(TaskKindCompact, "turn-2")
	_, err = session.steerInput(UserTurnInput{Content: "not allowed"}, "turn-2")
	if !errors.As(err, &steerErr) || steerErr.Kind != SteerInputActiveNotSteerable || steerErr.TaskKind != TaskKindCompact {
		t.Fatalf("compact error = %#v", err)
	}
}

func testActiveTurn(kind TaskKind, turnID protocol.TurnID) *ActiveTurn {
	context := &turn.TurnContext{SessionID: testutil.SessionID(1), ThreadID: testutil.ThreadID(1), TurnID: turnID, Provider: "mock", Model: "model", CWD: "/workspace", Mode: turn.ModeKindDefault}
	var task SessionTask
	if kind == TaskKindCompact {
		task = &compactTask{}
	} else {
		task = &regularTask{}
	}
	return &ActiveTurn{Task: &RunningTask{task: task, kind: kind, context: context}, State: newTurnState()}
}
