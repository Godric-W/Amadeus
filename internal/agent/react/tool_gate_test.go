package react

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type controlledExecutor struct {
	entered chan string
	release chan struct{}
	mutex   sync.Mutex
	active  int
	maximum int
}

func (executor *controlledExecutor) Execute(ctx context.Context, call tool.Call) (ToolOutcome, error) {
	executor.mutex.Lock()
	executor.active++
	if executor.active > executor.maximum {
		executor.maximum = executor.active
	}
	executor.mutex.Unlock()
	executor.entered <- call.ID
	select {
	case <-executor.release:
	case <-ctx.Done():
	}
	executor.mutex.Lock()
	executor.active--
	executor.mutex.Unlock()
	return ToolOutcome{CallID: call.ID, ToolName: call.Name, Status: ToolOutcomeSucceeded, Result: tool.Result{CallID: call.ID, ToolName: call.Name, Text: call.ID}}, ctx.Err()
}

func TestToolExecutionGateRunsSharedCallsConcurrentlyAndKeepsOrder(t *testing.T) {
	controlled := &controlledExecutor{entered: make(chan string, 3), release: make(chan struct{})}
	gate, err := newToolExecutionGate(controlled, 2)
	if err != nil {
		t.Fatalf("create tool execution gate: %v", err)
	}
	calls := []tool.Call{
		tool.NewCall("first", "read_file", json.RawMessage(`{"path":"same"}`)),
		tool.NewCall("second", "read_file", json.RawMessage(`{"path":"same"}`)),
	}
	resultChannel := make(chan []indexedToolExecution, 1)
	go func() {
		results, _ := gate.Execute(context.Background(), calls, []tool.Spec{sharedReadSpec()})
		resultChannel <- results
	}()
	waitEntered(t, controlled.entered)
	waitEntered(t, controlled.entered)
	close(controlled.release)
	results := <-resultChannel
	if controlled.maximum != 2 || len(results) != 2 || results[0].outcome.CallID != "first" || results[1].outcome.CallID != "second" {
		t.Fatalf("unexpected shared results: max=%d results=%#v", controlled.maximum, results)
	}
}

func TestToolExecutionGateTreatsExclusiveCallsAsOrderedBarriers(t *testing.T) {
	controlled := &controlledExecutor{entered: make(chan string, 3), release: make(chan struct{}, 3)}
	gate, err := newToolExecutionGate(controlled, 3)
	if err != nil {
		t.Fatalf("create tool execution gate: %v", err)
	}
	calls := []tool.Call{
		tool.NewCall("read_before", "read_file", json.RawMessage(`{"path":"a"}`)),
		tool.NewCall("patch", "apply_patch", json.RawMessage(`{"patch":"document"}`)),
		tool.NewCall("read_after", "read_file", json.RawMessage(`{"path":"b"}`)),
	}
	done := make(chan struct{})
	go func() {
		_, _ = gate.Execute(context.Background(), calls, []tool.Spec{sharedReadSpec(), exclusiveSpec("apply_patch", tool.SideEffectWrite)})
		close(done)
	}()
	for _, expected := range []string{"read_before", "patch", "read_after"} {
		if entered := waitEntered(t, controlled.entered); entered != expected {
			t.Fatalf("barrier order changed: got %q, want %q", entered, expected)
		}
		controlled.release <- struct{}{}
	}
	<-done
	if controlled.maximum != 1 {
		t.Fatalf("exclusive barrier overlapped calls: max=%d", controlled.maximum)
	}
}

func TestToolExecutionGateHonorsMaximumParallelism(t *testing.T) {
	controlled := &controlledExecutor{entered: make(chan string, 4), release: make(chan struct{})}
	gate, err := newToolExecutionGate(controlled, 2)
	if err != nil {
		t.Fatalf("create tool execution gate: %v", err)
	}
	calls := []tool.Call{
		tool.NewCall("one", "read_file", json.RawMessage(`{"path":"1"}`)),
		tool.NewCall("two", "read_file", json.RawMessage(`{"path":"2"}`)),
		tool.NewCall("three", "read_file", json.RawMessage(`{"path":"3"}`)),
	}
	done := make(chan struct{})
	go func() {
		_, _ = gate.Execute(context.Background(), calls, []tool.Spec{sharedReadSpec()})
		close(done)
	}()
	waitEntered(t, controlled.entered)
	waitEntered(t, controlled.entered)
	select {
	case third := <-controlled.entered:
		t.Fatalf("parallelism limit exceeded by %q", third)
	case <-time.After(30 * time.Millisecond):
	}
	close(controlled.release)
	<-done
	if controlled.maximum != 2 {
		t.Fatalf("unexpected maximum concurrency: %d", controlled.maximum)
	}
}

func TestToolExecutionGateTreatsUnknownToolAsExclusive(t *testing.T) {
	if got := toolConcurrency(tool.NewCall("call", "unknown", nil), nil); got != tool.ToolConcurrencyExclusive {
		t.Fatalf("unknown tool concurrency = %q, want exclusive", got)
	}
}

func TestToolExecutionGateReturnsSharedExecutorInfrastructureError(t *testing.T) {
	want := errors.New("executor unavailable")
	gate, err := newToolExecutionGate(callExecutorFunc(func(context.Context, tool.Call) (ToolOutcome, error) {
		return ToolOutcome{}, want
	}), 2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = gate.Execute(context.Background(), []tool.Call{
		tool.NewCall("first", "read_file", json.RawMessage(`{"path":"a"}`)),
		tool.NewCall("second", "read_file", json.RawMessage(`{"path":"b"}`)),
	}, []tool.Spec{sharedReadSpec()})
	if !errors.Is(err, want) {
		t.Fatalf("shared infrastructure error = %v, want %v", err, want)
	}
}

type callExecutorFunc func(context.Context, tool.Call) (ToolOutcome, error)

func (function callExecutorFunc) Execute(ctx context.Context, call tool.Call) (ToolOutcome, error) {
	return function(ctx, call)
}

func sharedReadSpec() tool.Spec {
	return tool.Spec{Name: "read_file", SideEffect: tool.SideEffectRead, Concurrency: tool.ToolConcurrencyShared}
}

func exclusiveSpec(name string, effect tool.SideEffect) tool.Spec {
	return tool.Spec{Name: name, SideEffect: effect, Concurrency: tool.ToolConcurrencyExclusive}
}

func waitEntered(t *testing.T, entered <-chan string) string {
	t.Helper()
	select {
	case callID := <-entered:
		return callID
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tool execution")
		return ""
	}
}
