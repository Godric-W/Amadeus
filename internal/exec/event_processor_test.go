package exec

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
)

type processorThread struct {
	events     chan protocol.Event
	terminated chan struct{}

	mu          sync.Mutex
	submissions []protocol.Op
	submitted   chan struct{}
}

func newProcessorThread() *processorThread {
	return &processorThread{
		events: make(chan protocol.Event, 8), terminated: make(chan struct{}),
		submitted: make(chan struct{}, 8),
	}
}

func (thread *processorThread) Io() agentsession.SessionIo {
	return agentsession.SessionIo{Events: thread.events, Terminated: thread.terminated}
}

func (thread *processorThread) Submit(_ context.Context, op protocol.Op) error {
	thread.mu.Lock()
	thread.submissions = append(thread.submissions, op)
	thread.mu.Unlock()
	thread.submitted <- struct{}{}
	return nil
}

func TestEventProcessorWaitsForMatchingTerminal(t *testing.T) {
	thread := newProcessorThread()
	thread.events <- protocol.Event{Msg: protocol.TurnStartedEvent{TurnID: "turn-1"}}
	thread.events <- protocol.Event{Msg: protocol.TurnCompleteEvent{TurnID: "turn-2"}}
	thread.events <- protocol.Event{Msg: protocol.TurnCompleteEvent{TurnID: "turn-1"}}
	if err := ProcessEvents(context.Background(), thread, EventProcessorOptions{}); err != nil {
		t.Fatalf("process matching terminal: %v", err)
	}
}

func TestEventProcessorCancellationSubmitsOneInterruptAndWaitsForAbort(t *testing.T) {
	thread := newProcessorThread()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- ProcessEvents(ctx, thread, EventProcessorOptions{}) }()
	cancel()
	select {
	case <-thread.submitted:
	case <-time.After(time.Second):
		t.Fatal("interrupt was not submitted")
	}
	thread.events <- protocol.Event{Msg: protocol.TurnAbortedEvent{TurnID: "turn-1", Reason: "result: cancelled"}}
	select {
	case err := <-result:
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 130 || !exitErr.AlreadyReported() {
			t.Fatalf("unexpected cancellation result: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("processor did not finish after abort")
	}
	thread.mu.Lock()
	defer thread.mu.Unlock()
	if len(thread.submissions) != 1 {
		t.Fatalf("cancellation submitted %d operations", len(thread.submissions))
	}
	if _, ok := thread.submissions[0].(protocol.InterruptOp); !ok {
		t.Fatalf("cancellation submitted %T", thread.submissions[0])
	}
}

func TestEventProcessorAnswersRequestUserInputBeforeTerminal(t *testing.T) {
	thread := newProcessorThread()
	thread.events <- protocol.Event{Msg: protocol.TurnStartedEvent{TurnID: "turn-1"}}
	first := protocol.RequestUserInputEvent{
		RequestID: "request-1", CallID: "call-1",
		RequestUserInputArgs: protocol.RequestUserInputArgs{Questions: []protocol.RequestUserInputQuestion{{
			ID: "choice", Header: "Choice", Question: "Choose one",
			Options: []protocol.RequestUserInputOption{
				{Label: "First", Description: "first option"},
				{Label: "Second", Description: "second option"},
			},
		}}},
	}
	second := first
	second.RequestID, second.CallID = "request-2", "call-2"
	thread.events <- protocol.Event{Msg: first}
	thread.events <- protocol.Event{Msg: second}
	thread.events <- protocol.Event{Msg: protocol.TurnCompleteEvent{TurnID: "turn-1"}}
	var output bytes.Buffer
	if err := ProcessEvents(context.Background(), thread, EventProcessorOptions{
		Input: bytes.NewBufferString("1\n2\n"), Output: &output,
	}); err != nil {
		t.Fatal(err)
	}
	thread.mu.Lock()
	defer thread.mu.Unlock()
	if len(thread.submissions) != 2 {
		t.Fatalf("request_user_input submitted %d responses", len(thread.submissions))
	}
	firstAnswer, firstOK := thread.submissions[0].(protocol.UserInputAnswerOp)
	secondAnswer, secondOK := thread.submissions[1].(protocol.UserInputAnswerOp)
	if !firstOK || !secondOK || firstAnswer.Response.Answers["choice"].Answers[0] != "First" || secondAnswer.Response.Answers["choice"].Answers[0] != "Second" {
		t.Fatalf("unexpected request_user_input responses: %#v", thread.submissions)
	}
}
