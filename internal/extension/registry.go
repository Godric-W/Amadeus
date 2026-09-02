package extension

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

type EventSink interface {
	Emit(protocol.Event)
}

type NoopEventSink struct{}

func (NoopEventSink) Emit(protocol.Event) {}

type EventBinding interface {
	Close()
}

type EventBinder interface {
	EventSink
	Bind(protocol.ThreadID, chan<- EventDelivery, <-chan struct{}) (EventBinding, error)
}

type OrderedEventSink interface {
	EventSink
	EmitOrdered(context.Context, protocol.Event) error
}

type EventDelivery struct {
	Event     protocol.Event
	Delivered chan<- error
}

type routedEventTarget struct {
	inbox chan<- EventDelivery
	done  <-chan struct{}
	token uint64
}

type EventRouter struct {
	mu      sync.Mutex
	targets map[protocol.ThreadID]routedEventTarget
	next    uint64
}

func NewEventRouter() *EventRouter {
	return &EventRouter{targets: make(map[protocol.ThreadID]routedEventTarget)}
}

func (router *EventRouter) Bind(threadID protocol.ThreadID, inbox chan<- EventDelivery, done <-chan struct{}) (EventBinding, error) {
	if router == nil || threadID.IsZero() || inbox == nil || done == nil {
		return nil, errors.New("extension event binding is incomplete")
	}
	router.mu.Lock()
	defer router.mu.Unlock()
	if _, exists := router.targets[threadID]; exists {
		return nil, fmt.Errorf("extension event target already bound for thread %s", threadID)
	}
	router.next++
	token := router.next
	router.targets[threadID] = routedEventTarget{inbox: inbox, done: done, token: token}
	return &eventBinding{router: router, threadID: threadID, token: token}, nil
}

func (router *EventRouter) Emit(event protocol.Event) {
	if router == nil || event.Msg == nil {
		return
	}
	threadID := protocol.ThreadIDOf(event.Msg)
	if threadID.IsZero() {
		return
	}
	router.mu.Lock()
	target, ok := router.targets[threadID]
	if ok {
		select {
		case target.inbox <- EventDelivery{Event: event}:
		case <-target.done:
		}
	}
	router.mu.Unlock()
}

func (router *EventRouter) EmitOrdered(ctx context.Context, event protocol.Event) error {
	if router == nil || event.Msg == nil {
		return errors.New("ordered extension event is incomplete")
	}
	threadID := protocol.ThreadIDOf(event.Msg)
	if threadID.IsZero() {
		return errors.New("ordered extension event thread ID is empty")
	}
	router.mu.Lock()
	target, ok := router.targets[threadID]
	router.mu.Unlock()
	if !ok {
		return fmt.Errorf("extension event target is not bound for thread %s", threadID)
	}
	delivered := make(chan error, 1)
	select {
	case target.inbox <- EventDelivery{Event: event, Delivered: delivered}:
	case <-target.done:
		return errors.New("extension event target is stopped")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-delivered:
		return err
	case <-target.done:
		return errors.New("extension event target stopped before delivery")
	case <-ctx.Done():
		return ctx.Err()
	}
}

type eventBinding struct {
	once     sync.Once
	router   *EventRouter
	threadID protocol.ThreadID
	token    uint64
}

func (binding *eventBinding) Close() {
	if binding == nil {
		return
	}
	binding.once.Do(func() {
		binding.router.mu.Lock()
		if target, ok := binding.router.targets[binding.threadID]; ok && target.token == binding.token {
			delete(binding.router.targets, binding.threadID)
		}
		binding.router.mu.Unlock()
	})
}

type Builder struct {
	eventSink       EventSink
	threadLifecycle []ThreadLifecycleContributor
	turnLifecycle   []TurnLifecycleContributor
	config          []ConfigContributor
	tokenUsage      []TokenUsageContributor
	toolLifecycle   []ToolLifecycleContributor
	tools           []ToolContributor
}

func NewBuilder(sink EventSink) *Builder {
	if sink == nil {
		sink = NoopEventSink{}
	}
	return &Builder{eventSink: sink}
}

func (builder *Builder) EventSink() EventSink {
	if builder == nil || builder.eventSink == nil {
		return NoopEventSink{}
	}
	return builder.eventSink
}

func (builder *Builder) ThreadLifecycle(value ThreadLifecycleContributor) {
	if value != nil {
		builder.threadLifecycle = append(builder.threadLifecycle, value)
	}
}

func (builder *Builder) TurnLifecycle(value TurnLifecycleContributor) {
	if value != nil {
		builder.turnLifecycle = append(builder.turnLifecycle, value)
	}
}

func (builder *Builder) Config(value ConfigContributor) {
	if value != nil {
		builder.config = append(builder.config, value)
	}
}

func (builder *Builder) TokenUsage(value TokenUsageContributor) {
	if value != nil {
		builder.tokenUsage = append(builder.tokenUsage, value)
	}
}

func (builder *Builder) ToolLifecycle(value ToolLifecycleContributor) {
	if value != nil {
		builder.toolLifecycle = append(builder.toolLifecycle, value)
	}
}

func (builder *Builder) Tools(value ToolContributor) {
	if value != nil {
		builder.tools = append(builder.tools, value)
	}
}

func (builder *Builder) Build() *Registry {
	if builder == nil {
		return EmptyRegistry()
	}
	return &Registry{
		eventSink:       builder.eventSink,
		threadLifecycle: append([]ThreadLifecycleContributor(nil), builder.threadLifecycle...),
		turnLifecycle:   append([]TurnLifecycleContributor(nil), builder.turnLifecycle...),
		config:          append([]ConfigContributor(nil), builder.config...),
		tokenUsage:      append([]TokenUsageContributor(nil), builder.tokenUsage...),
		toolLifecycle:   append([]ToolLifecycleContributor(nil), builder.toolLifecycle...),
		tools:           append([]ToolContributor(nil), builder.tools...),
	}
}

type Registry struct {
	eventSink       EventSink
	threadLifecycle []ThreadLifecycleContributor
	turnLifecycle   []TurnLifecycleContributor
	config          []ConfigContributor
	tokenUsage      []TokenUsageContributor
	toolLifecycle   []ToolLifecycleContributor
	tools           []ToolContributor
}

var (
	emptyOnce sync.Once
	empty     *Registry
)

func EmptyRegistry() *Registry {
	emptyOnce.Do(func() { empty = NewBuilder(NoopEventSink{}).Build() })
	return empty
}

func (registry *Registry) EventSink() EventSink {
	if registry == nil || registry.eventSink == nil {
		return NoopEventSink{}
	}
	return registry.eventSink
}

func (registry *Registry) ThreadLifecycle() []ThreadLifecycleContributor {
	if registry == nil {
		return nil
	}
	return append([]ThreadLifecycleContributor(nil), registry.threadLifecycle...)
}

func (registry *Registry) TurnLifecycle() []TurnLifecycleContributor {
	if registry == nil {
		return nil
	}
	return append([]TurnLifecycleContributor(nil), registry.turnLifecycle...)
}

func (registry *Registry) ConfigContributors() []ConfigContributor {
	if registry == nil {
		return nil
	}
	return append([]ConfigContributor(nil), registry.config...)
}

func (registry *Registry) TokenUsage() []TokenUsageContributor {
	if registry == nil {
		return nil
	}
	return append([]TokenUsageContributor(nil), registry.tokenUsage...)
}

func (registry *Registry) ToolLifecycle() []ToolLifecycleContributor {
	if registry == nil {
		return nil
	}
	return append([]ToolLifecycleContributor(nil), registry.toolLifecycle...)
}

func (registry *Registry) Tools() []ToolContributor {
	if registry == nil {
		return nil
	}
	return append([]ToolContributor(nil), registry.tools...)
}
