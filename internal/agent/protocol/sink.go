package protocol

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// EventSink is the only runtime output port for product events. Implementations
// must preserve the order in which a single producer publishes events.
type EventSink interface {
	Publish(context.Context, Event) error
}

type MemorySink struct {
	mu     sync.RWMutex
	events []Event
	nextID uint64
}

func NewMemorySink() *MemorySink { return &MemorySink{} }

func (sink *MemorySink) Publish(ctx context.Context, event Event) error {
	if sink == nil {
		return errors.New("protocol memory sink is nil")
	}
	if ctx == nil {
		return errors.New("event publish context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if event.ID == "" {
		sink.nextID++
		event.ID = SubmissionID(fmt.Sprintf("memory-event-%d", sink.nextID))
	}
	if err := event.Validate(); err != nil {
		return err
	}
	sink.events = append(sink.events, event)
	return nil
}

func (sink *MemorySink) Snapshot() []Event {
	if sink == nil {
		return nil
	}
	sink.mu.RLock()
	defer sink.mu.RUnlock()
	return append([]Event(nil), sink.events...)
}

// ScopedSink binds one submission and turn to all events produced by a task.
type ScopedSink struct {
	parent   EventSink
	eventID  SubmissionID
	threadID ThreadID
	turnID   TurnID
}

func NewScopedSink(parent EventSink, eventID SubmissionID, threadID ThreadID, turnID TurnID) (*ScopedSink, error) {
	if parent == nil {
		return nil, errors.New("scoped event sink parent is nil")
	}
	if eventID == "" || threadID.IsZero() {
		return nil, errors.New("scoped event sink identity is incomplete")
	}
	return &ScopedSink{parent: parent, eventID: eventID, threadID: threadID, turnID: turnID}, nil
}

func (sink *ScopedSink) Publish(ctx context.Context, event Event) error {
	if sink == nil || sink.parent == nil {
		return errors.New("scoped event sink is nil")
	}
	if event.ID == "" {
		event.ID = sink.eventID
	}
	event.Msg = ScopeEventMsg(event.Msg, sink.threadID, sink.turnID)
	return sink.parent.Publish(ctx, event)
}

func (sink *ScopedSink) ThreadID() ThreadID {
	if sink == nil {
		return ThreadID{}
	}
	return sink.threadID
}

func (sink *ScopedSink) TurnID() TurnID {
	if sink == nil {
		return ""
	}
	return sink.turnID
}
