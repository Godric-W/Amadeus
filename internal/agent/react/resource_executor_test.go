package react

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type controlledExecutor struct {
	entered chan string
	release chan struct{}
	mutex   sync.Mutex
	active  int
	maximum int
}

func (executor *controlledExecutor) Execute(ctx context.Context, call tool.Call) (ToolExecution, error) {
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
	return ToolExecution{
		Observation: engine.Observation{CallID: call.ID, ToolName: call.Name, Result: tool.Result{CallID: call.ID, ToolName: call.Name, Text: call.ID}},
		Evidence:    engine.Evidence{ID: engine.EvidenceID("tool:" + call.ID), Kind: engine.EvidenceTool, Source: call.Name, Summary: call.ID, Verified: true},
	}, ctx.Err()
}

func TestResourceExecutorRunsIndependentReadsConcurrentlyAndKeepsOrder(t *testing.T) {
	controlled := &controlledExecutor{entered: make(chan string, 3), release: make(chan struct{})}
	executor, err := newResourceExecutor(controlled, 2)
	if err != nil {
		t.Fatalf("create resource executor: %v", err)
	}
	calls := []tool.Call{
		tool.NewCall("first", "read_file", json.RawMessage(`{"path":"a"}`)),
		tool.NewCall("second", "read_file", json.RawMessage(`{"path":"b"}`)),
	}
	resultChannel := make(chan []indexedToolExecution, 1)
	go func() {
		results, _ := executor.Execute(context.Background(), calls, []tool.Spec{parallelReadSpec()})
		resultChannel <- results
	}()
	waitEntered(t, controlled.entered)
	waitEntered(t, controlled.entered)
	close(controlled.release)
	results := <-resultChannel
	if controlled.maximum != 2 || len(results) != 2 || results[0].execution.Observation.CallID != "first" || results[1].execution.Observation.CallID != "second" {
		t.Fatalf("unexpected parallel results: max=%d results=%#v", controlled.maximum, results)
	}
}

func TestResourceExecutorSerializesConflictingResources(t *testing.T) {
	controlled := &controlledExecutor{entered: make(chan string, 2), release: make(chan struct{}, 2)}
	executor, err := newResourceExecutor(controlled, 2)
	if err != nil {
		t.Fatalf("create resource executor: %v", err)
	}
	calls := []tool.Call{
		tool.NewCall("first", "read_file", json.RawMessage(`{"path":"same"}`)),
		tool.NewCall("second", "read_file", json.RawMessage(`{"path":"same"}`)),
	}
	done := make(chan struct{})
	go func() {
		_, _ = executor.Execute(context.Background(), calls, []tool.Spec{parallelReadSpec()})
		close(done)
	}()
	waitEntered(t, controlled.entered)
	select {
	case second := <-controlled.entered:
		t.Fatalf("conflicting call entered concurrently: %s", second)
	case <-time.After(30 * time.Millisecond):
	}
	controlled.release <- struct{}{}
	waitEntered(t, controlled.entered)
	controlled.release <- struct{}{}
	<-done
	if controlled.maximum != 1 {
		t.Fatalf("conflicting resources overlapped: max=%d", controlled.maximum)
	}
}

func TestResourceExecutorTreatsWritesAndExclusiveCallsAsBarriers(t *testing.T) {
	controlled := &controlledExecutor{entered: make(chan string, 3), release: make(chan struct{}, 3)}
	executor, err := newResourceExecutor(controlled, 3)
	if err != nil {
		t.Fatalf("create resource executor: %v", err)
	}
	calls := []tool.Call{
		tool.NewCall("read_before", "read_file", json.RawMessage(`{"path":"a"}`)),
		tool.NewCall("write", "write_file", json.RawMessage(`{"path":"b","content":"x"}`)),
		tool.NewCall("read_after", "read_file", json.RawMessage(`{"path":"c"}`)),
	}
	done := make(chan struct{})
	go func() {
		_, _ = executor.Execute(context.Background(), calls, []tool.Spec{parallelReadSpec(), {
			Name: "write_file", SideEffect: tool.SideEffectWrite, ParallelSafe: false,
			ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"path"}},
		}})
		close(done)
	}()
	for _, expected := range []string{"read_before", "write", "read_after"} {
		if entered := waitEntered(t, controlled.entered); entered != expected {
			t.Fatalf("barrier order changed: got %q, want %q", entered, expected)
		}
		controlled.release <- struct{}{}
	}
	<-done
	if controlled.maximum != 1 {
		t.Fatalf("barrier calls overlapped: max=%d", controlled.maximum)
	}
}

func TestResourceExecutorTreatsApplyPatchAsExclusiveSerialBarrier(t *testing.T) {
	controlled := &controlledExecutor{entered: make(chan string, 3), release: make(chan struct{}, 3)}
	executor, err := newResourceExecutor(controlled, 3)
	if err != nil {
		t.Fatalf("create resource executor: %v", err)
	}
	calls := []tool.Call{
		tool.NewCall("read_before", "read_file", json.RawMessage(`{"path":"a"}`)),
		tool.NewCall("patch", "apply_patch", json.RawMessage(`{"patch":"document"}`)),
		tool.NewCall("read_after", "read_file", json.RawMessage(`{"path":"b"}`)),
	}
	done := make(chan struct{})
	go func() {
		_, _ = executor.Execute(context.Background(), calls, []tool.Spec{parallelReadSpec(), {
			Name: "apply_patch", SideEffect: tool.SideEffectWrite, ParallelSafe: false,
			ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeExclusive},
		}})
		close(done)
	}()
	for _, expected := range []string{"read_before", "patch", "read_after"} {
		if entered := waitEntered(t, controlled.entered); entered != expected {
			t.Fatalf("apply_patch barrier order changed: got %q, want %q", entered, expected)
		}
		controlled.release <- struct{}{}
	}
	<-done
	if controlled.maximum != 1 {
		t.Fatalf("apply_patch barrier overlapped calls: max=%d", controlled.maximum)
	}
}

func TestResourceExecutorHonorsMaximumParallelism(t *testing.T) {
	controlled := &controlledExecutor{entered: make(chan string, 4), release: make(chan struct{})}
	executor, err := newResourceExecutor(controlled, 2)
	if err != nil {
		t.Fatalf("create resource executor: %v", err)
	}
	calls := []tool.Call{
		tool.NewCall("one", "read_file", json.RawMessage(`{"path":"1"}`)),
		tool.NewCall("two", "read_file", json.RawMessage(`{"path":"2"}`)),
		tool.NewCall("three", "read_file", json.RawMessage(`{"path":"3"}`)),
	}
	done := make(chan struct{})
	go func() {
		_, _ = executor.Execute(context.Background(), calls, []tool.Spec{parallelReadSpec()})
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

func parallelReadSpec() tool.Spec {
	return tool.Spec{
		Name: "read_file", SideEffect: tool.SideEffectRead, ParallelSafe: true,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"path"}},
	}
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
