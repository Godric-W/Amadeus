package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestPublishCancellationUnblocksNonCriticalEvent(t *testing.T) {
	session := testEventDeliverySession(1)
	session.events <- protocol.Event{ID: "occupied", Msg: protocol.WarningEvent{Message: "occupied"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := session.Publish(ctx, protocol.Event{ID: "warning", Msg: protocol.WarningEvent{Message: "warning"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestPublishCriticalEventIgnoresTaskCancellationUntilConsumerDrains(t *testing.T) {
	session := testEventDeliverySession(1)
	session.events <- protocol.Event{ID: "occupied", Msg: protocol.WarningEvent{Message: "occupied"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	go func() {
		time.Sleep(10 * time.Millisecond)
		<-session.events
	}()

	err := session.Publish(ctx, protocol.Event{ID: "terminal", Msg: protocol.TurnCompleteEvent{TurnID: "turn-1"}})
	if err != nil {
		t.Fatalf("critical event should be delivered after drain: %v", err)
	}
	select {
	case event := <-session.events:
		if event.ID != "terminal" {
			t.Fatalf("received unexpected event %q", event.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("critical event was not delivered")
	}
}

func TestPublishCriticalEventReportsBoundedDeliveryFailure(t *testing.T) {
	previous := criticalEventDeliveryTimeout
	criticalEventDeliveryTimeout = 15 * time.Millisecond
	defer func() { criticalEventDeliveryTimeout = previous }()

	session := testEventDeliverySession(1)
	session.events <- protocol.Event{ID: "occupied", Msg: protocol.WarningEvent{Message: "occupied"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := session.Publish(ctx, protocol.Event{ID: "approval", Msg: protocol.ApprovalRequestEvent{
		RequestID: "request-1", Approval: protocol.ApprovalRequest{ID: "request-1"},
	}})
	if !errors.Is(err, ErrCriticalEventDelivery) {
		t.Fatalf("expected critical delivery error, got %v", err)
	}

	session.publish(protocol.Event{ID: "request-input", Msg: protocol.RequestUserInputEvent{
		RequestID: "request-2", CallID: "call-2", RequestUserInputArgs: tool.RequestUserInputArgs{Questions: []tool.RequestUserInputQuestion{{
			ID: "choice", Header: "Choice", Question: "Choose", Options: []tool.RequestUserInputOption{{Label: "a", Description: "a"}, {Label: "b", Description: "b"}},
		}}},
	}})
	if !errors.Is(session.EventDeliveryError(), ErrCriticalEventDelivery) {
		t.Fatalf("publish helper did not retain delivery diagnostic: %v", session.EventDeliveryError())
	}
}

func TestCriticalEventClassification(t *testing.T) {
	cases := []struct {
		name     string
		message  protocol.EventMsg
		critical bool
	}{
		{name: "turn complete", message: protocol.TurnCompleteEvent{}, critical: true},
		{name: "turn aborted", message: protocol.TurnAbortedEvent{}, critical: true},
		{name: "item completed", message: protocol.ItemCompletedEvent{}, critical: true},
		{name: "approval", message: protocol.ApprovalRequestEvent{}, critical: true},
		{name: "user input", message: protocol.RequestUserInputEvent{}, critical: true},
		{name: "error", message: protocol.ErrorEvent{}, critical: true},
		{name: "delta", message: protocol.AgentMessageContentDeltaEvent{}, critical: false},
		{name: "warning", message: protocol.WarningEvent{}, critical: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := criticalEvent(testCase.message); got != testCase.critical {
				t.Fatalf("criticalEvent() = %v, want %v", got, testCase.critical)
			}
		})
	}
}

func testEventDeliverySession(capacity int) *Session {
	return &Session{
		ctx:        context.Background(),
		events:     make(chan protocol.Event, capacity),
		terminated: make(chan struct{}),
	}
}
