package react

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type fakeTool struct {
	spec      tool.Spec
	result    tool.Result
	err       error
	executed  int
	arguments json.RawMessage
}

func (candidate *fakeTool) Spec() tool.Spec { return candidate.spec.Clone() }

func (candidate *fakeTool) Execute(_ context.Context, arguments json.RawMessage) (tool.Result, error) {
	candidate.executed++
	candidate.arguments = append(json.RawMessage(nil), arguments...)
	return candidate.result.Clone(), candidate.err
}

func TestToolExecutorValidatesRepairsAndExecutes(t *testing.T) {
	candidate := &fakeTool{spec: executionSpec(), result: tool.Result{Text: "file contents"}}
	executor := newTestToolExecutor(t, candidate)
	call := tool.NewCall("call_1", "read_file", json.RawMessage(`{"path":"README.md"`))

	execution, err := executor.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("execute repaired call: %v", err)
	}
	if candidate.executed != 1 || string(candidate.arguments) != `{"path":"README.md"}` {
		t.Fatalf("tool did not receive normalized arguments: count=%d arguments=%s", candidate.executed, candidate.arguments)
	}
	if execution.Observation.CallID != "call_1" || execution.Observation.Result.CallID != "call_1" || execution.Observation.Result.ToolName != "read_file" || execution.Observation.Error != "" {
		t.Fatalf("unexpected successful observation: %#v", execution.Observation)
	}
	if execution.Evidence.ID != engine.EvidenceID("tool:call_1") || !execution.Evidence.Verified || execution.Evidence.Summary != "file contents" {
		t.Fatalf("unexpected successful evidence: %#v", execution.Evidence)
	}
	if execution.Observation.Duration != time.Second {
		t.Fatalf("unexpected execution duration: %s", execution.Observation.Duration)
	}
}

func TestToolExecutorDoesNotExecuteInvalidArguments(t *testing.T) {
	candidate := &fakeTool{spec: executionSpec()}
	executor := newTestToolExecutor(t, candidate)
	call := tool.NewCall("call_invalid", "read_file", json.RawMessage(`{"unknown":true}`))

	execution, err := executor.Execute(context.Background(), call)
	var argumentError *tool.ArgumentError
	if !errors.As(err, &argumentError) {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if candidate.executed != 0 {
		t.Fatalf("invalid arguments executed the tool %d time(s)", candidate.executed)
	}
	if execution.Observation.Error == "" || execution.Evidence.Verified || execution.Evidence.Summary == "" {
		t.Fatalf("invalid arguments did not form failure observation/evidence: %#v", execution)
	}
}

func TestToolExecutorPreservesFailedResultAsObservation(t *testing.T) {
	expectedErr := errors.New("permission denied")
	candidate := &fakeTool{
		spec:   executionSpec(),
		result: tool.Result{Text: "partial output", Partial: true, Metadata: map[string]any{"exit_code": 1}},
		err:    expectedErr,
	}
	executor := newTestToolExecutor(t, candidate)

	execution, err := executor.Execute(context.Background(), tool.NewCall("call_failed", "read_file", json.RawMessage(`{"path":"secret"}`)))
	if !errors.Is(err, expectedErr) {
		t.Fatalf("unexpected execution error: %v", err)
	}
	if execution.Observation.Result.Text != "partial output" || !execution.Observation.Result.Partial || execution.Observation.Error != expectedErr.Error() {
		t.Fatalf("failed result was not preserved: %#v", execution.Observation)
	}
	if execution.Evidence.Verified || execution.Evidence.Summary != "tool failed: permission denied" {
		t.Fatalf("unexpected failed evidence: %#v", execution.Evidence)
	}
}

func TestToolExecutorReturnsObservationForUnknownTool(t *testing.T) {
	registry := tool.NewRegistry()
	executor, err := NewToolExecutor(registry, tool.NewArgumentValidator())
	if err != nil {
		t.Fatalf("create executor: %v", err)
	}
	executor.now = fixedClock(time.Unix(0, 0), time.Unix(0, 0))

	execution, err := executor.Execute(context.Background(), tool.NewCall("call_missing", "missing", json.RawMessage(`{}`)))
	if err == nil || execution.Observation.Error == "" || execution.Evidence.Verified {
		t.Fatalf("unexpected unknown tool execution: execution=%#v err=%v", execution, err)
	}
}

func newTestToolExecutor(t *testing.T, candidate *fakeTool) *ToolExecutor {
	t.Helper()
	registry := tool.NewRegistry()
	if err := registry.Register(candidate); err != nil {
		t.Fatalf("register fake tool: %v", err)
	}
	executor, err := NewToolExecutor(registry, tool.NewArgumentValidator())
	if err != nil {
		t.Fatalf("create tool executor: %v", err)
	}
	executor.now = fixedClock(time.Unix(0, 0), time.Unix(1, 0))
	return executor
}

func executionSpec() tool.Spec {
	return tool.Spec{
		Name:         "read_file",
		Description:  "Read a file",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`),
		SideEffect:   tool.SideEffectRead,
		ParallelSafe: true,
		Idempotent:   true,
		ResourceStrategy: tool.ResourceStrategy{
			Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"/path"},
		},
	}
}

func fixedClock(values ...time.Time) func() time.Time {
	index := 0
	return func() time.Time {
		if index >= len(values) {
			return values[len(values)-1]
		}
		value := values[index]
		index++
		return value
	}
}

func TestToolExecutionResultDoesNotShareMetadata(t *testing.T) {
	candidate := &fakeTool{spec: executionSpec(), result: tool.Result{Metadata: map[string]any{"key": "value"}}}
	executor := newTestToolExecutor(t, candidate)
	execution, err := executor.Execute(context.Background(), tool.NewCall("call_metadata", "read_file", json.RawMessage(`{"path":"README.md"}`)))
	if err != nil {
		t.Fatalf("execute metadata call: %v", err)
	}
	execution.Observation.Result.Metadata["key"] = "changed"
	if reflect.DeepEqual(candidate.result.Metadata, execution.Observation.Result.Metadata) {
		t.Fatal("observation metadata shares tool result storage")
	}
}
