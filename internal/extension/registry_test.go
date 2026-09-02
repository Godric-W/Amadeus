package extension

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

type testValue struct{ Name string }

func TestExtensionDataUsesTypedScopeValues(t *testing.T) {
	data, err := NewData("thread-1")
	if err != nil {
		t.Fatal(err)
	}
	value := GetOrInit(data, func() testValue { return testValue{Name: "first"} })
	if value == nil || value.Name != "first" || data.LevelID() != "thread-1" {
		t.Fatalf("value=%#v level=%q", value, data.LevelID())
	}
	if again := GetOrInit(data, func() testValue { return testValue{Name: "second"} }); again != value {
		t.Fatal("typed value was replaced")
	}
	previous, ok := Set(data, testValue{Name: "replacement"})
	if !ok || previous.Name != "first" {
		t.Fatalf("previous=%#v ok=%v", previous, ok)
	}
	current, ok := Get[testValue](data)
	if !ok || current.Name != "replacement" {
		t.Fatalf("current=%#v ok=%v", current, ok)
	}
}

func TestEventRouterBindsOrderedDeliveryAndRejectsStaleBinding(t *testing.T) {
	router := NewEventRouter()
	threadID := testutil.ThreadID(1)
	inbox := make(chan EventDelivery, 2)
	done := make(chan struct{})
	binding, err := router.Bind(threadID, inbox, done)
	if err != nil {
		t.Fatal(err)
	}
	event := protocol.Event{Msg: protocol.WarningEvent{ThreadID: threadID, Message: "ordered"}}
	result := make(chan error, 1)
	go func() { result <- router.EmitOrdered(context.Background(), event) }()
	delivery := <-inbox
	if delivery.Event.Msg != event.Msg || delivery.Delivered == nil {
		t.Fatalf("ordered delivery = %#v", delivery)
	}
	delivery.Delivered <- nil
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	binding.Close()
	replacementInbox := make(chan EventDelivery, 1)
	replacementDone := make(chan struct{})
	replacement, err := router.Bind(threadID, replacementInbox, replacementDone)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	binding.Close()
	router.Emit(protocol.Event{Msg: protocol.WarningEvent{ThreadID: threadID, Message: "replacement"}})
	select {
	case delivery := <-replacementInbox:
		if delivery.Event.Msg.(protocol.WarningEvent).Message != "replacement" {
			t.Fatalf("replacement delivery = %#v", delivery)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement binding did not receive event")
	}
}

type orderContributor struct {
	ThreadLifecycleDefaults
	id    string
	calls *[]string
}

func (contributor orderContributor) OnThreadIdle(context.Context, ThreadIdleInput) error {
	*contributor.calls = append(*contributor.calls, contributor.id)
	return nil
}

func TestRegistryPreservesRegistrationOrderAndReturnsCopies(t *testing.T) {
	var calls []string
	builder := NewBuilder(nil)
	builder.ThreadLifecycle(orderContributor{id: "queue", calls: &calls})
	builder.ThreadLifecycle(orderContributor{id: "goal", calls: &calls})
	registry := builder.Build()
	contributors := registry.ThreadLifecycle()
	contributors[0] = nil
	for _, contributor := range registry.ThreadLifecycle() {
		if err := contributor.OnThreadIdle(context.Background(), ThreadIdleInput{}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(calls, []string{"queue", "goal"}) {
		t.Fatalf("calls = %#v", calls)
	}
}
