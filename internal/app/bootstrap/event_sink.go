package bootstrap

import (
	"context"

	"github.com/Godric-W/Amadeus/internal/agent/event"
)

type taskIterationEventSink struct {
	downstream event.Sink
}

func newTaskIterationEventSink(downstream event.Sink) event.Sink {
	return taskIterationEventSink{downstream: downstream}
}

func (sink taskIterationEventSink) Publish(ctx context.Context, item event.Event) error {
	if item == nil {
		return nil
	}
	switch item.Type() {
	case event.TypeTextDelta, event.TypeReasoningDelta:
		if event.TaskIDFromContext(ctx) != "" {
			return nil
		}
		return sink.downstream.Publish(ctx, item)
	default:
		return sink.downstream.Publish(ctx, item)
	}
}
