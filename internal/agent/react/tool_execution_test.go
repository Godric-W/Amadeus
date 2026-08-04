package react

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/tool/builtin"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type fakeTool struct {
	spec      tool.Spec
	result    tool.Result
	err       error
	executed  int
	arguments json.RawMessage
}

type fakeAuthorizer struct {
	calls []tool.Call
	err   error
}

func (authorizer *fakeAuthorizer) Authorize(_ context.Context, _ tool.Spec, call tool.Call) error {
	authorizer.calls = append(authorizer.calls, call.Clone())
	return authorizer.err
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
	if execution.Evidence.ID != EvidenceID("tool:call_1") || !execution.Evidence.Verified || execution.Evidence.Summary != "file contents" {
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

func TestToolExecutorAuthorizesNormalizedArgumentsBeforeExecution(t *testing.T) {
	expectedErr := errors.New("approval denied")
	authorizer := &fakeAuthorizer{err: expectedErr}
	candidate := &fakeTool{spec: executionSpec()}
	registry := tool.NewRegistry()
	if err := registry.Register(candidate); err != nil {
		t.Fatalf("register fake tool: %v", err)
	}
	executor, err := NewToolExecutor(registry, tool.NewArgumentValidator(), authorizer)
	if err != nil {
		t.Fatalf("create authorized tool executor: %v", err)
	}

	execution, err := executor.Execute(context.Background(), tool.NewCall("call_auth", "read_file", json.RawMessage(`{"path":"README.md"`)))
	if !errors.Is(err, expectedErr) {
		t.Fatalf("unexpected authorization error: %v", err)
	}
	if candidate.executed != 0 {
		t.Fatalf("denied tool executed %d time(s)", candidate.executed)
	}
	if len(authorizer.calls) != 1 || string(authorizer.calls[0].Arguments) != `{"path":"README.md"}` {
		t.Fatalf("authorizer did not receive normalized call: %#v", authorizer.calls)
	}
	if execution.Observation.Error != expectedErr.Error() || execution.Evidence.Verified {
		t.Fatalf("authorization denial did not produce failure evidence: %#v", execution)
	}
}

func TestToolExecutorPublishesStartedAndCompletedEvents(t *testing.T) {
	candidate := &fakeTool{spec: executionSpec(), result: tool.Result{Text: "file contents", Partial: true}}
	registry := tool.NewRegistry()
	if err := registry.Register(candidate); err != nil {
		t.Fatalf("register fake tool: %v", err)
	}
	sink := event.NewMemorySink()
	executor, err := NewToolExecutorWithOptions(registry, tool.NewArgumentValidator(), ToolExecutorOptions{Events: sink})
	if err != nil {
		t.Fatalf("create eventful tool executor: %v", err)
	}
	executor.now = fixedClock(time.Unix(0, 0), time.Unix(0, int64(25*time.Millisecond)))
	if _, err := executor.Execute(context.Background(), tool.NewCall("call_events", "read_file", json.RawMessage(`{"path":"README.md"}`))); err != nil {
		t.Fatalf("execute eventful tool: %v", err)
	}
	events := sink.Snapshot()
	if len(events) != 2 {
		t.Fatalf("unexpected tool event count: %#v", events)
	}
	started, ok := events[0].(event.ToolCallStarted)
	if !ok || started.CallID != "call_events" || started.ToolName != "read_file" {
		t.Fatalf("unexpected tool started event: %#v", events[0])
	}
	completed, ok := events[1].(event.ToolCallCompleted)
	if !ok || !completed.Success || !completed.Partial || completed.Summary != "file contents" || completed.Duration != 25*time.Millisecond {
		t.Fatalf("unexpected tool completed event: %#v", events[1])
	}
}

func TestToolExecutorEventFailurePreventsExecutionBeforeStart(t *testing.T) {
	expected := errors.New("event output failed")
	candidate := &fakeTool{spec: executionSpec()}
	registry := tool.NewRegistry()
	if err := registry.Register(candidate); err != nil {
		t.Fatalf("register fake tool: %v", err)
	}
	executor, err := NewToolExecutorWithOptions(registry, tool.NewArgumentValidator(), ToolExecutorOptions{Events: failingEventSink{err: expected}})
	if err != nil {
		t.Fatalf("create failing-event executor: %v", err)
	}
	if _, err := executor.Execute(context.Background(), tool.NewCall("call_event_error", "read_file", json.RawMessage(`{"path":"README.md"}`))); !errors.Is(err, expected) {
		t.Fatalf("unexpected event failure: %v", err)
	}
	if candidate.executed != 0 {
		t.Fatalf("tool executed after start event failure: %d", candidate.executed)
	}
}

func TestNewToolExecutorValidatesAuthorizerOptions(t *testing.T) {
	registry := tool.NewRegistry()
	validator := tool.NewArgumentValidator()
	if executor, err := NewToolExecutor(registry, validator, nil); err == nil || executor != nil {
		t.Fatalf("unexpected nil authorizer result: executor=%#v err=%v", executor, err)
	}
	if executor, err := NewToolExecutor(registry, validator, &fakeAuthorizer{}, &fakeAuthorizer{}); err == nil || executor != nil {
		t.Fatalf("unexpected multiple authorizer result: executor=%#v err=%v", executor, err)
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
	if execution.Evidence.Verified || execution.Evidence.Summary != "tool partially applied: partial output: permission denied" {
		t.Fatalf("unexpected failed evidence: %#v", execution.Evidence)
	}
}

func TestToolExecutorPreservesApplyPatchCancellationWithoutWrites(t *testing.T) {
	rootPath := t.TempDir()
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	registry, err := builtin.NewMVPRegistry(root, builtin.DefaultMVPOptions())
	if err != nil {
		t.Fatalf("create MVP registry: %v", err)
	}
	executor, err := NewToolExecutor(registry, tool.NewArgumentValidator())
	if err != nil {
		t.Fatalf("create tool executor: %v", err)
	}
	patch := "*** Begin Patch v1\n*** Add File: cancelled.txt\n+must not exist\n*** End Patch\n"
	arguments, err := json.Marshal(map[string]string{"patch": patch})
	if err != nil {
		t.Fatalf("encode patch arguments: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	execution, err := executor.Execute(ctx, tool.NewCall("patch-cancelled", "apply_patch", arguments))
	if !errors.Is(err, context.Canceled) || execution.Observation.Error != context.Canceled.Error() || execution.Evidence.Verified {
		t.Fatalf("unexpected cancelled patch execution: execution=%#v err=%v", execution, err)
	}
	if _, statErr := os.Stat(filepath.Join(rootPath, "cancelled.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cancelled patch changed project: %v", statErr)
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

type failingEventSink struct{ err error }

func (sink failingEventSink) Publish(context.Context, event.Event) error { return sink.err }

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
