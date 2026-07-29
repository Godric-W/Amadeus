package event

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
)

var ErrNilEvent = errors.New("event is nil")

type MemorySink struct {
	mutex  sync.RWMutex
	events []Event
}

func NewMemorySink() *MemorySink {
	return &MemorySink{}
}

func (sink *MemorySink) Publish(ctx context.Context, runtimeEvent Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if runtimeEvent == nil || isNilEvent(runtimeEvent) {
		return ErrNilEvent
	}
	if !runtimeEvent.Type().Valid() {
		return fmt.Errorf("invalid event type %q", runtimeEvent.Type())
	}

	sink.mutex.Lock()
	sink.events = append(sink.events, runtimeEvent)
	sink.mutex.Unlock()
	return nil
}

func (sink *MemorySink) Snapshot() []Event {
	sink.mutex.RLock()
	snapshot := append([]Event(nil), sink.events...)
	sink.mutex.RUnlock()
	return snapshot
}

func (sink *MemorySink) Len() int {
	sink.mutex.RLock()
	length := len(sink.events)
	sink.mutex.RUnlock()
	return length
}

func isNilEvent(runtimeEvent Event) bool {
	value := reflect.ValueOf(runtimeEvent)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

var _ Sink = (*MemorySink)(nil)
