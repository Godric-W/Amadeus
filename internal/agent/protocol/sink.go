package protocol

import (
	"context"
	"errors"
	"sync"

	"github.com/Godric-W/Amadeus/internal/rollout"
)

// EventSink is the only runtime output port for product events. Implementations
// must preserve the order in which a single producer publishes events.
type EventSink interface {
	Publish(context.Context, SessionEvent) error
}

// MemorySink is useful for deterministic tests and small in-process adapters.
// It intentionally stores the already-scoped SessionEvent rather than adding
// metadata after publication.
type MemorySink struct {
	mu     sync.RWMutex
	events []SessionEvent
}

func NewMemorySink() *MemorySink { return &MemorySink{} }

func (sink *MemorySink) Publish(ctx context.Context, event SessionEvent) error {
	if sink == nil {
		return errors.New("protocol memory sink is nil")
	}
	if ctx == nil {
		return errors.New("event publish context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// MemorySink is also used by direct Agent composition tests, where there is
	// no Session boundary to add routing scope. Keep the persisted/test event
	// valid without weakening Session.Publish validation.
	if event.ThreadID == "" {
		event.ThreadID = "memory-thread"
	}
	if err := event.Validate(); err != nil {
		return err
	}
	sink.mu.Lock()
	sink.events = append(sink.events, event)
	sink.mu.Unlock()
	return nil
}

func (sink *MemorySink) Snapshot() []SessionEvent {
	if sink == nil {
		return nil
	}
	sink.mu.RLock()
	defer sink.mu.RUnlock()
	return append([]SessionEvent(nil), sink.events...)
}

// ScopedSink adds only the stable routing fields that belong to the Session
// boundary. It does not mutate EventMessage payloads or inject reflection
// metadata.
type ScopedSink struct {
	parent   EventSink
	threadID rollout.ThreadID
	turnID   rollout.TurnID
}

func NewScopedSink(parent EventSink, threadID rollout.ThreadID, turnID rollout.TurnID) (*ScopedSink, error) {
	if parent == nil {
		return nil, errors.New("scoped event sink parent is nil")
	}
	if threadID == "" {
		return nil, errors.New("scoped event sink thread ID is empty")
	}
	return &ScopedSink{parent: parent, threadID: threadID, turnID: turnID}, nil
}

func (sink *ScopedSink) Publish(ctx context.Context, event SessionEvent) error {
	if sink == nil || sink.parent == nil {
		return errors.New("scoped event sink is nil")
	}
	if event.ThreadID == "" {
		event.ThreadID = sink.threadID
	}
	if event.TurnID == "" {
		event.TurnID = sink.turnID
	}
	return sink.parent.Publish(ctx, event)
}

func (sink *ScopedSink) ThreadID() rollout.ThreadID {
	if sink == nil {
		return ""
	}
	return sink.threadID
}

func (sink *ScopedSink) TurnID() rollout.TurnID {
	if sink == nil {
		return ""
	}
	return sink.turnID
}
