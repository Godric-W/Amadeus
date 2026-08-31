package session

import (
	"context"
	"errors"
	"testing"

	"github.com/Godric-W/Amadeus/internal/protocol"
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

func TestUserMessageSettingsApplyOnlyAfterSteerAdmission(t *testing.T) {
	session := newTestSession(nil, nil)
	session.state.Configuration.Mode = ModeKindDefault
	session.active = testActiveTurn(TaskKindCompact, "turn-1")

	_, err := session.admitUserMessage("submission-1", protocol.UserInputOp{
		Content: "continue",
		ThreadSettings: protocol.ThreadSettingsOverrides{
			CollaborationMode: &protocol.CollaborationMode{Mode: protocol.ModeKindPlan},
		},
	})
	var steerErr *SteerInputError
	if !errors.As(err, &steerErr) || steerErr.Kind != SteerInputActiveNotSteerable {
		t.Fatalf("admission error = %#v", err)
	}
	if got := session.Configuration().Mode; got != ModeKindDefault {
		t.Fatalf("rejected steer changed mode to %q", got)
	}
}

func TestSteeredUserMessageAppliesSettingsAfterSuccessfulAdmission(t *testing.T) {
	session := newTestSession(nil, nil)
	session.ctx = context.Background()
	session.events = make(chan protocol.Event, 2)
	session.terminated = make(chan struct{})
	session.state.Configuration.Mode = ModeKindDefault
	session.active = testActiveTurn(TaskKindRegular, "turn-1")

	admission, err := session.admitUserMessage("submission-1", protocol.UserInputOp{
		Content: "continue",
		ThreadSettings: protocol.ThreadSettingsOverrides{
			CollaborationMode: &protocol.CollaborationMode{Mode: protocol.ModeKindPlan},
		},
	})
	if err != nil || admission.Kind != protocol.UserMessageAdmissionSteered || admission.TurnID != "turn-1" {
		t.Fatalf("admission = %#v, err=%v", admission, err)
	}
	if got := session.Configuration().Mode; got != ModeKindPlan {
		t.Fatalf("successful steer did not apply mode: %q", got)
	}
	select {
	case event := <-session.events:
		if _, ok := event.Msg.(protocol.ThreadSettingsAppliedEvent); !ok {
			t.Fatalf("settings event = %#v", event.Msg)
		}
	default:
		t.Fatal("successful steer did not publish settings event")
	}
}

func TestInvalidStandaloneThreadSettingsPublishCorrelatedError(t *testing.T) {
	session := newTestSession(nil, nil)
	session.ctx = context.Background()
	session.events = make(chan protocol.Event, 2)
	session.terminated = make(chan struct{})
	session.state.Configuration.Mode = ModeKindDefault
	session.handleSubmission(protocol.Submission{ID: "submission-1", Op: protocol.ThreadSettingsOp{Mode: "invalid"}})
	if got := session.Configuration().Mode; got != ModeKindDefault {
		t.Fatalf("invalid settings changed mode to %q", got)
	}
	select {
	case event := <-session.events:
		errorEvent, ok := event.Msg.(protocol.ErrorEvent)
		if !ok || event.ID != "submission-1" || errorEvent.Code != "invalid_thread_settings" {
			t.Fatalf("invalid settings event = %#v", event)
		}
	default:
		t.Fatal("invalid settings did not publish error event")
	}
}

func testActiveTurn(kind TaskKind, turnID protocol.TurnID) *ActiveTurn {
	context := &TurnContext{SessionID: testutil.SessionID(1), ThreadID: testutil.ThreadID(1), TurnID: turnID, Provider: "mock", Model: "model", CWD: "/workspace", Mode: ModeKindDefault}
	var task SessionTask
	if kind == TaskKindCompact {
		task = &compactTask{}
	} else {
		task = &regularTask{}
	}
	return &ActiveTurn{Task: &RunningTask{task: task, kind: kind, context: context}, State: newTurnState()}
}
