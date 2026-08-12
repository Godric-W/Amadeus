package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type routerTestHandler struct {
	name     string
	parallel bool
	handle   func(context.Context, Invocation) (Output, error)
}

func (handler *routerTestHandler) Spec() Spec {
	return Spec{
		Name:        handler.name,
		Description: "router test handler",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`),
		SideEffect:  SideEffectRead,
		Idempotent:  true,
	}
}

func (handler *routerTestHandler) SupportsParallelToolCalls() bool { return handler.parallel }

func (handler *routerTestHandler) Handle(ctx context.Context, invocation Invocation) (Output, error) {
	if handler.handle == nil {
		return Output{Text: string(invocation.Call.Payload)}, nil
	}
	return handler.handle(ctx, invocation)
}

type routerPermissionError struct{ message string }

func (err routerPermissionError) Error() string         { return err.message }
func (err routerPermissionError) ToolErrorKind() string { return "permission_required" }

type recordingLifecycleObserver struct {
	mutex      sync.Mutex
	started    []ToolCall
	completed  []ToolExecution
	startedErr error
	finishErr  error
}

func (observer *recordingLifecycleObserver) ToolCallStarted(_ context.Context, _ Spec, call ToolCall) error {
	observer.mutex.Lock()
	defer observer.mutex.Unlock()
	observer.started = append(observer.started, call.Clone())
	return observer.startedErr
}

func (observer *recordingLifecycleObserver) ToolCallCompleted(_ context.Context, execution ToolExecution) error {
	observer.mutex.Lock()
	defer observer.mutex.Unlock()
	observer.completed = append(observer.completed, execution)
	return observer.finishErr
}

func TestRouterRepairsValidatesAndExecutesOnce(t *testing.T) {
	var calls atomic.Int32
	handler := &routerTestHandler{name: "read_test", parallel: true, handle: func(_ context.Context, invocation Invocation) (Output, error) {
		calls.Add(1)
		if string(invocation.Call.Payload) != `{"value":"ok"}` {
			t.Fatalf("arguments were not normalized: %s", invocation.Call.Payload)
		}
		return Output{Text: "done", Metadata: map[string]any{"path": "README.md"}}, nil
	}}
	router := newRouterForTest(t, RouterOptions{}, handler)
	execution, err := router.Execute(context.Background(), NewCall("call-1", handler.name, json.RawMessage(`{"value":"ok",`)))
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || execution.Outcome.Status != ToolCallCompleted || execution.Output.CallID != "call-1" || execution.Output.ToolName != handler.name || execution.Output.Text != "done" {
		t.Fatalf("unexpected execution: %#v calls=%d", execution, calls.Load())
	}
	execution.Output.Metadata["path"] = "changed"
	if execution.Outcome.Metadata["path"] != "README.md" {
		t.Fatalf("outcome metadata aliases output metadata: %#v", execution)
	}
}

func TestRouterReturnsModelVisibleLookupAndArgumentFailures(t *testing.T) {
	handler := &routerTestHandler{name: "read_test", parallel: true}
	router := newRouterForTest(t, RouterOptions{}, handler)
	tests := []struct {
		name string
		call ToolCall
		kind string
	}{
		{name: "unknown", call: NewCall("unknown-1", "missing", json.RawMessage(`{}`)), kind: "not_registered"},
		{name: "invalid", call: NewCall("invalid-1", handler.name, json.RawMessage(`{"value":12}`)), kind: "invalid_arguments"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			execution, err := router.Execute(context.Background(), test.call)
			if err != nil {
				t.Fatal(err)
			}
			if execution.Outcome.Status != ToolCallFailed || execution.Outcome.Error == nil || execution.Outcome.Error.Kind != test.kind || execution.Output.Text == "" {
				t.Fatalf("unexpected failure: %#v", execution)
			}
		})
	}
}

func TestRouterClassifiesDeniedPartialAndInterruptedCalls(t *testing.T) {
	handlers := []Handler{
		&routerTestHandler{name: "denied", handle: func(context.Context, Invocation) (Output, error) {
			return Output{}, routerPermissionError{message: "outside writable roots"}
		}},
		&routerTestHandler{name: "partial", handle: func(context.Context, Invocation) (Output, error) {
			return Output{Text: "first operation applied", Partial: true}, errors.New("second operation failed")
		}},
		&routerTestHandler{name: "cancelled", handle: func(ctx context.Context, _ Invocation) (Output, error) {
			return Output{}, ctx.Err()
		}},
	}
	router := newRouterForTest(t, RouterOptions{}, handlers...)
	denied, _ := router.Execute(context.Background(), routerCall("denied-1", "denied"))
	if denied.Outcome.Status != ToolCallDenied || denied.Outcome.Blocking || denied.Outcome.Error.Kind != "permission_required" {
		t.Fatalf("unexpected denied execution: %#v", denied)
	}
	partial, _ := router.Execute(context.Background(), routerCall("partial-1", "partial"))
	if partial.Outcome.Status != ToolCallFailed || !partial.Output.Partial || partial.Output.Text != "first operation applied" {
		t.Fatalf("unexpected partial execution: %#v", partial)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	interrupted, _ := router.Execute(ctx, routerCall("cancelled-1", "cancelled"))
	if interrupted.Outcome.Status != ToolCallInterrupted || interrupted.Outcome.Error == nil {
		t.Fatalf("unexpected interrupted execution: %#v", interrupted)
	}
}

func TestRouterParallelLimitOrderAndExclusiveBarrier(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	var mutex sync.Mutex
	sequence := make([]string, 0, 8)
	parallel := &routerTestHandler{name: "parallel", parallel: true, handle: func(_ context.Context, invocation Invocation) (Output, error) {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		mutex.Lock()
		sequence = append(sequence, "start:"+invocation.Call.ID)
		mutex.Unlock()
		if invocation.Call.ID == "p1" {
			time.Sleep(35 * time.Millisecond)
		} else {
			time.Sleep(10 * time.Millisecond)
		}
		active.Add(-1)
		mutex.Lock()
		sequence = append(sequence, "finish:"+invocation.Call.ID)
		mutex.Unlock()
		return Output{Text: invocation.Call.ID}, nil
	}}
	exclusive := &routerTestHandler{name: "exclusive", parallel: false, handle: func(_ context.Context, invocation Invocation) (Output, error) {
		if active.Load() != 0 {
			return Output{}, fmt.Errorf("exclusive call overlapped %d parallel calls", active.Load())
		}
		mutex.Lock()
		sequence = append(sequence, "exclusive:"+invocation.Call.ID)
		mutex.Unlock()
		return Output{Text: invocation.Call.ID}, nil
	}}
	router := newRouterForTest(t, RouterOptions{MaxParallel: 2}, parallel, exclusive)
	calls := []ToolCall{routerCall("p1", "parallel"), routerCall("p2", "parallel"), routerCall("p3", "parallel"), routerCall("x1", "exclusive"), routerCall("p4", "parallel")}
	executions, err := router.ExecuteBatch(context.Background(), calls, nil)
	if err != nil {
		t.Fatal(err)
	}
	if maximum.Load() != 2 {
		t.Fatalf("max parallelism = %d, want 2", maximum.Load())
	}
	for index, execution := range executions {
		if execution.Call.ID != calls[index].ID || execution.Outcome.Status != ToolCallCompleted {
			t.Fatalf("execution order/status mismatch at %d: %#v", index, executions)
		}
	}
	mutex.Lock()
	defer mutex.Unlock()
	exclusiveIndex := indexOf(sequence, "exclusive:x1")
	if exclusiveIndex < 0 || indexOf(sequence, "finish:p1") > exclusiveIndex || indexOf(sequence, "finish:p2") > exclusiveIndex || indexOf(sequence, "finish:p3") > exclusiveIndex || indexOf(sequence, "start:p4") < exclusiveIndex {
		t.Fatalf("exclusive barrier was not preserved: %v", sequence)
	}
}

func TestRouterRecordsNormalizedCallsBeforeAnySideEffect(t *testing.T) {
	var handled atomic.Int32
	handler := &routerTestHandler{name: "write_test", parallel: false, handle: func(context.Context, Invocation) (Output, error) {
		handled.Add(1)
		return Output{Text: "changed"}, nil
	}}
	router := newRouterForTest(t, RouterOptions{}, handler)
	want := errors.New("rollout unavailable")
	_, err := router.ExecuteBatch(context.Background(), []ToolCall{
		NewCall("call-1", handler.name, json.RawMessage(`{"value":"ok",`)),
	}, func(_ context.Context, calls []ToolCall) error {
		if len(calls) != 1 || string(calls[0].Payload) != `{"value":"ok"}` {
			t.Fatalf("recorder did not receive normalized calls: %#v", calls)
		}
		return want
	})
	if !errors.Is(err, want) || handled.Load() != 0 {
		t.Fatalf("record failure did not fail closed: handled=%d err=%v", handled.Load(), err)
	}
}

func TestRouterInvalidCallDoesNotCancelValidSibling(t *testing.T) {
	var handled atomic.Int32
	handler := &routerTestHandler{name: "read_test", parallel: true, handle: func(context.Context, Invocation) (Output, error) {
		handled.Add(1)
		return Output{Text: "ok"}, nil
	}}
	router := newRouterForTest(t, RouterOptions{MaxParallel: 2}, handler)
	var recorded []ToolCall
	executions, err := router.ExecuteBatch(context.Background(), []ToolCall{
		NewCall("bad", handler.name, json.RawMessage(`{"value":`)),
		routerCall("good", handler.name),
	}, func(_ context.Context, calls []ToolCall) error {
		recorded = append([]ToolCall(nil), calls...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if handled.Load() != 1 || len(executions) != 2 || executions[0].Outcome.Error == nil || executions[0].Outcome.Error.Kind != "invalid_arguments" || executions[1].Outcome.Status != ToolCallCompleted {
		t.Fatalf("unexpected independent batch result: handled=%d executions=%#v", handled.Load(), executions)
	}
	if len(recorded) != 2 || string(recorded[0].Payload) != `null` || string(recorded[1].Payload) != `{"value":"ok"}` {
		t.Fatalf("unexpected canonical recorder calls: %#v", recorded)
	}
}

func TestRouterGateWaitIsCancellable(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	exclusive := &routerTestHandler{name: "exclusive", parallel: false, handle: func(context.Context, Invocation) (Output, error) {
		close(entered)
		<-release
		return Output{Text: "released"}, nil
	}}
	router := newRouterForTest(t, RouterOptions{}, exclusive)
	firstDone := make(chan struct{})
	go func() {
		_, _ = router.Execute(context.Background(), routerCall("first", "exclusive"))
		close(firstDone)
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	second, err := router.Execute(ctx, routerCall("second", "exclusive"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Outcome.Status != ToolCallInterrupted || time.Since(started) > 250*time.Millisecond {
		t.Fatalf("cancelled waiter did not return promptly: %#v elapsed=%s", second, time.Since(started))
	}
	close(release)
	<-firstDone
}

func TestRouterPublishesLifecycleAroundNormalizedExecution(t *testing.T) {
	observer := &recordingLifecycleObserver{}
	handler := &routerTestHandler{name: "read_test", parallel: true}
	router := newRouterForTest(t, RouterOptions{Observer: observer}, handler)
	execution, err := router.Execute(context.Background(), NewCall("call-1", handler.name, json.RawMessage(`{"value":"ok",`)))
	if err != nil {
		t.Fatal(err)
	}
	if len(observer.started) != 1 || string(observer.started[0].Payload) != `{"value":"ok"}` || len(observer.completed) != 1 || observer.completed[0].Call.ID != execution.Call.ID {
		t.Fatalf("unexpected lifecycle: started=%#v completed=%#v", observer.started, observer.completed)
	}
}

func TestRouterRejectsInvisibleHandlerAndPopulatesInvocationMetadata(t *testing.T) {
	var received Invocation
	handler := &routerTestHandler{name: "conditional", parallel: true, handle: func(_ context.Context, invocation Invocation) (Output, error) {
		received = invocation
		return Output{Text: "ok"}, nil
	}}
	registry := NewRegistry()
	if err := registry.RegisterWithRegistration(handler, Registration{Exposure: ExposureConditional, Condition: "enabled"}); err != nil {
		t.Fatal(err)
	}
	hidden, err := NewRouter(registry, NewArgumentValidator(), RouterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := hidden.Execute(context.Background(), routerCall("hidden", handler.name))
	if err != nil || execution.Outcome.Error == nil || execution.Outcome.Error.Kind != "not_registered" {
		t.Fatalf("invisible Handler executed: %#v err=%v", execution, err)
	}
	visible, err := NewRouter(registry, NewArgumentValidator(), RouterOptions{Visibility: map[string]bool{"enabled": true}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithInvocationMetadata(context.Background(), InvocationMetadata{SessionID: "session-1", TurnID: "run-1", Source: ToolCallSourceUser})
	execution, err = visible.Execute(ctx, routerCall("visible", handler.name))
	if err != nil || execution.Outcome.Status != ToolCallCompleted {
		t.Fatalf("visible Handler failed: %#v err=%v", execution, err)
	}
	if received.SessionID != "session-1" || received.TurnID != "run-1" || received.Source != ToolCallSourceUser {
		t.Fatalf("Invocation metadata was not populated: %#v", received)
	}
}

func newRouterForTest(t *testing.T, options RouterOptions, handlers ...Handler) *Router {
	t.Helper()
	registry := NewRegistry()
	for _, handler := range handlers {
		if err := registry.Register(handler); err != nil {
			t.Fatal(err)
		}
	}
	router, err := NewRouter(registry, NewArgumentValidator(), options)
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func routerCall(id, name string) ToolCall {
	return NewCall(id, name, json.RawMessage(`{"value":"ok"}`))
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}
