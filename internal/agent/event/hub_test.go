package event

import (
	"context"
	"errors"
	"testing"
)

func TestHubFansOutFilteredEnrichedEvents(t *testing.T) {
	memory := NewMemorySink()
	hub, err := NewHub(memory)
	if err != nil {
		t.Fatalf("create event hub: %v", err)
	}
	defer hub.Close()
	subscription, err := hub.Subscribe(SubscriptionOptions{Types: []Type{TypeTextDelta}, Buffer: 1})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer subscription.Close()

	ctx := WithMetadata(context.Background(), Metadata{SessionID: "session-1", TurnID: "run-1", Iteration: 3})
	if err := hub.Publish(ctx, TextDelta{LLMCallID: "call-1", Delta: "hello"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := hub.Publish(ctx, UsageUpdated{LLMCallID: "call-1"}); err != nil {
		t.Fatalf("publish filtered event: %v", err)
	}

	got := (<-subscription.Events).(TextDelta)
	if got.SessionID != "session-1" || got.TurnID != "run-1" || got.Iteration != 3 || got.LLMCallID != "call-1" || got.Delta != "hello" {
		t.Fatalf("unexpected subscriber event: %#v", got)
	}
	if memory.Len() != 2 {
		t.Fatalf("critical sink received %d events, want 2", memory.Len())
	}
}

func TestHubFansOutToMultipleSubscribers(t *testing.T) {
	hub, err := NewHub()
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	first, err := hub.Subscribe(SubscriptionOptions{Buffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := hub.Subscribe(SubscriptionOptions{Buffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := hub.Publish(context.Background(), TurnCompleted{TurnID: "run-1", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	for index, subscription := range []*Subscription{first, second} {
		got, ok := (<-subscription.Events).(TurnCompleted)
		if !ok || got.TurnID != "run-1" {
			t.Fatalf("subscriber %d received %#v", index, got)
		}
	}
}

func TestHubReportsCriticalBackpressureAndCountsNonCriticalDrops(t *testing.T) {
	hub, err := NewHub()
	if err != nil {
		t.Fatalf("create event hub: %v", err)
	}
	defer hub.Close()
	subscription, err := hub.Subscribe(SubscriptionOptions{Buffer: 1})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer subscription.Close()

	if err := hub.Publish(context.Background(), TextDelta{Delta: "first"}); err != nil {
		t.Fatalf("fill subscriber buffer: %v", err)
	}
	if err := hub.Publish(context.Background(), TextDelta{Delta: "dropped"}); err != nil {
		t.Fatalf("publish non-critical event: %v", err)
	}
	if subscription.Dropped() != 1 {
		t.Fatalf("unexpected dropped count: %d", subscription.Dropped())
	}
	if err := hub.Publish(context.Background(), ToolCallCompleted{CallID: "call-1", ToolName: "read"}); !errors.Is(err, ErrSubscriberBackpressure) {
		t.Fatalf("unexpected critical backpressure error: %v", err)
	}
}

func TestHubCloseClosesSubscriptionsAndRejectsPublish(t *testing.T) {
	hub, err := NewHub()
	if err != nil {
		t.Fatalf("create event hub: %v", err)
	}
	subscription, err := hub.Subscribe(SubscriptionOptions{Buffer: 1})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := hub.Close(); err != nil {
		t.Fatalf("close hub: %v", err)
	}
	if _, ok := <-subscription.Events; ok {
		t.Fatal("subscription channel remained open")
	}
	if err := hub.Publish(context.Background(), TurnCompleted{}); !errors.Is(err, ErrHubClosed) {
		t.Fatalf("unexpected publish-after-close error: %v", err)
	}
}

type failingSink struct{ err error }

func (sink failingSink) Publish(context.Context, Event) error { return sink.err }

func TestHubPropagatesCriticalSinkErrors(t *testing.T) {
	want := errors.New("sink failed")
	hub, err := NewHub(failingSink{err: want})
	if err != nil {
		t.Fatalf("create event hub: %v", err)
	}
	defer hub.Close()
	if err := hub.Publish(context.Background(), TurnCompleted{}); !errors.Is(err, want) {
		t.Fatalf("unexpected sink error: %v", err)
	}
}
