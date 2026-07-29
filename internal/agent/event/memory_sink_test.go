package event

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
)

type invalidEvent struct{}

func (invalidEvent) Type() Type {
	return Type("invalid")
}

func TestMemorySinkPreservesPublishOrder(t *testing.T) {
	sink := NewMemorySink()
	events := []Event{
		TurnStarted{TurnID: "turn_1", Model: llm.ModelInfo{Provider: "openai", Name: "test-model"}},
		TextDelta{TurnID: "turn_1", ResponseID: "response_1", Delta: "hello"},
		ReasoningDelta{TurnID: "turn_1", ResponseID: "response_1", Delta: "thinking"},
		UsageUpdated{TurnID: "turn_1", ResponseID: "response_1", Usage: llm.Usage{TotalTokens: 8}},
		TurnCompleted{TurnID: "turn_1", ResponseID: "response_1", FinishReason: llm.FinishReasonStop},
	}
	for _, runtimeEvent := range events {
		if err := sink.Publish(context.Background(), runtimeEvent); err != nil {
			t.Fatalf("publish event: %v", err)
		}
	}

	snapshot := sink.Snapshot()
	if len(snapshot) != len(events) {
		t.Fatalf("unexpected event count: got %d, want %d", len(snapshot), len(events))
	}
	for index, expected := range events {
		if snapshot[index] != expected {
			t.Fatalf("unexpected event at %d: got %#v, want %#v", index, snapshot[index], expected)
		}
	}
}

func TestMemorySinkSnapshotIsSliceIsolated(t *testing.T) {
	sink := NewMemorySink()
	if err := sink.Publish(context.Background(), TextDelta{Delta: "original"}); err != nil {
		t.Fatalf("publish event: %v", err)
	}
	snapshot := sink.Snapshot()
	snapshot[0] = TextDelta{Delta: "changed"}

	stored := sink.Snapshot()
	if stored[0].(TextDelta).Delta != "original" {
		t.Fatalf("snapshot mutation changed stored event: %#v", stored[0])
	}
}

func TestMemorySinkSupportsConcurrentPublish(t *testing.T) {
	sink := NewMemorySink()
	const eventCount = 100
	var waitGroup sync.WaitGroup
	for index := 0; index < eventCount; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if err := sink.Publish(context.Background(), TextDelta{Delta: "chunk"}); err != nil {
				t.Errorf("publish event: %v", err)
			}
		}()
	}
	waitGroup.Wait()
	if sink.Len() != eventCount {
		t.Fatalf("unexpected concurrent event count: got %d, want %d", sink.Len(), eventCount)
	}
}

func TestMemorySinkRejectsCancelledContextAndInvalidEvents(t *testing.T) {
	sink := NewMemorySink()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sink.Publish(ctx, TextDelta{Delta: "ignored"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancelled publish error: %v", err)
	}
	if err := sink.Publish(context.Background(), nil); !errors.Is(err, ErrNilEvent) {
		t.Fatalf("unexpected nil event error: %v", err)
	}
	var typedNil *TextDelta
	if err := sink.Publish(context.Background(), typedNil); !errors.Is(err, ErrNilEvent) {
		t.Fatalf("unexpected typed nil event error: %v", err)
	}
	if err := sink.Publish(context.Background(), invalidEvent{}); err == nil {
		t.Fatal("expected invalid event type error")
	}
	if sink.Len() != 0 {
		t.Fatalf("rejected events were stored: %#v", sink.Snapshot())
	}
}
