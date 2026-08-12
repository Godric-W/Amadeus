package event

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
)

var (
	ErrHubClosed              = errors.New("event hub is closed")
	ErrSubscriberBackpressure = errors.New("event subscriber buffer is full")
)

type SubscriptionOptions struct {
	Types  []Type
	Buffer int
}

type Subscription struct {
	Events  <-chan Event
	close   func()
	dropped *atomic.Uint64
	once    sync.Once
}

func (subscription *Subscription) Close() {
	if subscription == nil {
		return
	}
	subscription.once.Do(func() {
		if subscription.close != nil {
			subscription.close()
		}
	})
}

func (subscription *Subscription) Dropped() uint64 {
	if subscription == nil || subscription.dropped == nil {
		return 0
	}
	return subscription.dropped.Load()
}

type subscriber struct {
	mutex   sync.Mutex
	events  chan Event
	types   map[Type]struct{}
	dropped atomic.Uint64
	closed  bool
}

func (subscriber *subscriber) accepts(eventType Type) bool {
	return len(subscriber.types) == 0 || containsType(subscriber.types, eventType)
}

func (subscriber *subscriber) publish(runtimeEvent Event) error {
	if !subscriber.accepts(runtimeEvent.Type()) {
		return nil
	}
	subscriber.mutex.Lock()
	defer subscriber.mutex.Unlock()
	if subscriber.closed {
		return nil
	}
	select {
	case subscriber.events <- runtimeEvent:
		return nil
	default:
		if criticalEvent(runtimeEvent.Type()) {
			return fmt.Errorf("%w: %s", ErrSubscriberBackpressure, runtimeEvent.Type())
		}
		subscriber.dropped.Add(1)
		return nil
	}
}

func (subscriber *subscriber) close() {
	subscriber.mutex.Lock()
	if !subscriber.closed {
		subscriber.closed = true
		close(subscriber.events)
	}
	subscriber.mutex.Unlock()
}

type Hub struct {
	mutex       sync.RWMutex
	sinks       []Sink
	subscribers map[uint64]*subscriber
	nextID      uint64
	closed      bool
}

func NewHub(sinks ...Sink) (*Hub, error) {
	for index, sink := range sinks {
		if sink == nil || isNilSink(sink) {
			return nil, fmt.Errorf("event hub sink %d is nil", index)
		}
	}
	return &Hub{sinks: append([]Sink(nil), sinks...), subscribers: make(map[uint64]*subscriber)}, nil
}

func (hub *Hub) Publish(ctx context.Context, runtimeEvent Event) error {
	if hub == nil {
		return ErrHubClosed
	}
	if ctx == nil {
		return errors.New("event publish context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if runtimeEvent == nil || isNilEvent(runtimeEvent) {
		return ErrNilEvent
	}
	if !runtimeEvent.Type().Valid() {
		return fmt.Errorf("invalid event type %q", runtimeEvent.Type())
	}
	enriched, err := WithEventMetadata(runtimeEvent, MetadataFromContext(ctx))
	if err != nil {
		return err
	}

	hub.mutex.RLock()
	if hub.closed {
		hub.mutex.RUnlock()
		return ErrHubClosed
	}
	sinks := append([]Sink(nil), hub.sinks...)
	subscribers := make([]*subscriber, 0, len(hub.subscribers))
	for _, candidate := range hub.subscribers {
		subscribers = append(subscribers, candidate)
	}
	hub.mutex.RUnlock()

	var publishErr error
	for _, sink := range sinks {
		publishErr = errors.Join(publishErr, sink.Publish(ctx, enriched))
	}
	for _, subscriber := range subscribers {
		publishErr = errors.Join(publishErr, subscriber.publish(enriched))
	}
	return publishErr
}

func (hub *Hub) Subscribe(options SubscriptionOptions) (*Subscription, error) {
	if hub == nil {
		return nil, ErrHubClosed
	}
	if options.Buffer <= 0 {
		options.Buffer = 64
	}
	types := make(map[Type]struct{}, len(options.Types))
	for _, eventType := range options.Types {
		if !eventType.Valid() {
			return nil, fmt.Errorf("invalid subscription event type %q", eventType)
		}
		types[eventType] = struct{}{}
	}
	candidate := &subscriber{events: make(chan Event, options.Buffer), types: types}

	hub.mutex.Lock()
	if hub.closed {
		hub.mutex.Unlock()
		return nil, ErrHubClosed
	}
	hub.nextID++
	id := hub.nextID
	hub.subscribers[id] = candidate
	hub.mutex.Unlock()

	subscription := &Subscription{Events: candidate.events, dropped: &candidate.dropped}
	subscription.close = func() {
		hub.mutex.Lock()
		if current, ok := hub.subscribers[id]; ok && current == candidate {
			delete(hub.subscribers, id)
		}
		hub.mutex.Unlock()
		candidate.close()
	}
	return subscription, nil
}

func (hub *Hub) Close() error {
	if hub == nil {
		return nil
	}
	hub.mutex.Lock()
	if hub.closed {
		hub.mutex.Unlock()
		return nil
	}
	hub.closed = true
	subscribers := make([]*subscriber, 0, len(hub.subscribers))
	for id, candidate := range hub.subscribers {
		subscribers = append(subscribers, candidate)
		delete(hub.subscribers, id)
	}
	hub.mutex.Unlock()
	for _, candidate := range subscribers {
		candidate.close()
	}
	return nil
}

func containsType(types map[Type]struct{}, eventType Type) bool {
	_, ok := types[eventType]
	return ok
}

func criticalEvent(eventType Type) bool {
	switch eventType {
	case TypeToolCallStarted, TypeToolCallCompleted, TypeApprovalRequested, TypeApprovalResolved,
		TypeErrorOccurred, TypeTurnCompleted:
		return true
	default:
		return false
	}
}

func isNilSink(sink Sink) bool {
	value := reflect.ValueOf(sink)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

var _ Sink = (*Hub)(nil)
