package react

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type outcomeTool struct {
	spec      tool.Spec
	result    tool.Result
	err       error
	prepare   func(context.Context, tool.Call) (tool.PreparedCall, error)
	arguments json.RawMessage
	calls     int
}

func (candidate *outcomeTool) Spec() tool.Spec { return candidate.spec.Clone() }

func (candidate *outcomeTool) Prepare(ctx context.Context, call tool.Call) (tool.PreparedCall, error) {
	if candidate.prepare != nil {
		return candidate.prepare(ctx, call)
	}
	return tool.PreparePassthrough(call, append(json.RawMessage(nil), call.Arguments...))
}
func (candidate *outcomeTool) Execute(_ context.Context, prepared tool.PreparedCall) (tool.Result, error) {
	candidate.calls++
	candidate.arguments = prepared.Call().Arguments
	return candidate.result.Clone(), candidate.err
}

type outcomeAuthorizer struct{ err error }

func (authorizer outcomeAuthorizer) Authorize(context.Context, tool.Spec, tool.PreparedCall) error {
	return authorizer.err
}

type outcomeKindError struct{ kind string }

func (err outcomeKindError) Error() string         { return err.kind }
func (err outcomeKindError) ToolErrorKind() string { return err.kind }

type outcomeHook struct {
	calls  int
	err    error
	result tool.Result
}

func (hook *outcomeHook) After(_ context.Context, _ tool.Spec, _ tool.Call, result tool.Result) error {
	hook.calls++
	hook.result = result.Clone()
	return hook.err
}

func TestToolExecutorReturnsSuccessfulOutcomeAndEvents(t *testing.T) {
	candidate := newOutcomeTool()
	sink := event.NewMemorySink()
	executor := newOutcomeExecutor(t, candidate, ToolExecutorOptions{Events: sink})
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	executor.now = func() time.Time { value := now; now = now.Add(time.Second); return value }
	outcome, err := executor.Execute(context.Background(), tool.NewCall("call_1", "read_file", json.RawMessage(`{"path":"README.md",}`)))
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Succeeded() || outcome.CallID != "call_1" || outcome.Result.Text != "file contents" || outcome.Duration != time.Second {
		t.Fatalf("unexpected successful outcome: %#v", outcome)
	}
	if string(candidate.arguments) != `{"path":"README.md"}` {
		t.Fatalf("tool did not receive normalized arguments: %s", candidate.arguments)
	}
	if events := sink.Snapshot(); len(events) != 2 || events[0].Type() != event.TypeToolCallStarted || events[1].Type() != event.TypeToolCallCompleted {
		t.Fatalf("unexpected tool events: %#v", events)
	}
}

func TestToolExecutorMapsApprovalDenialWithoutExecuting(t *testing.T) {
	candidate := newOutcomeTool()
	executor := newOutcomeExecutor(t, candidate, ToolExecutorOptions{Authorizer: outcomeAuthorizer{err: errors.New("denied")}})
	outcome, err := executor.Execute(context.Background(), tool.NewCall("call", "read_file", json.RawMessage(`{"path":"README.md"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != ToolOutcomeDenied || outcome.Error == nil || outcome.Error.Kind != "approval_denied" || candidate.calls != 0 {
		t.Fatalf("unexpected denied outcome: %#v calls=%d", outcome, candidate.calls)
	}
}

func TestToolExecutorPreservesTypedAuthorizationDenial(t *testing.T) {
	candidate := newOutcomeTool()
	executor := newOutcomeExecutor(t, candidate, ToolExecutorOptions{Authorizer: outcomeAuthorizer{err: outcomeKindError{kind: "path_denied"}}})
	outcome, err := executor.Execute(context.Background(), tool.NewCall("call", "read_file", json.RawMessage(`{"path":"README.md"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != ToolOutcomeDenied || outcome.Error == nil || outcome.Error.Kind != "path_denied" || candidate.calls != 0 {
		t.Fatalf("unexpected typed denial outcome: %#v calls=%d", outcome, candidate.calls)
	}
}

func TestToolExecutorPreservesPartialFailedResult(t *testing.T) {
	candidate := newOutcomeTool()
	candidate.result = tool.Result{Text: "partial output", Partial: true, Metadata: map[string]any{"changed": 1}}
	candidate.err = errors.New("permission denied")
	executor := newOutcomeExecutor(t, candidate, ToolExecutorOptions{})
	outcome, err := executor.Execute(context.Background(), tool.NewCall("call", "read_file", json.RawMessage(`{"path":"README.md"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != ToolOutcomeFailed || !outcome.Partial || outcome.Result.Text != "partial output" || outcome.ErrorMessage() != "permission denied" {
		t.Fatalf("unexpected failed outcome: %#v", outcome)
	}
}

func TestToolExecutorMapsCancellationToInterruptedOutcome(t *testing.T) {
	candidate := newOutcomeTool()
	candidate.err = context.Canceled
	executor := newOutcomeExecutor(t, candidate, ToolExecutorOptions{})
	outcome, err := executor.Execute(context.Background(), tool.NewCall("call", "read_file", json.RawMessage(`{"path":"README.md"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != ToolOutcomeInterrupted || outcome.Error == nil || outcome.Error.Kind != "interrupted" {
		t.Fatalf("unexpected interrupted outcome: %#v", outcome)
	}
}

func TestToolExecutorRejectsPrepareMutation(t *testing.T) {
	candidate := newOutcomeTool()
	candidate.prepare = func(_ context.Context, call tool.Call) (tool.PreparedCall, error) {
		call.Arguments = json.RawMessage(`{"path":"changed"}`)
		return tool.PreparePassthrough(call, nil)
	}
	executor := newOutcomeExecutor(t, candidate, ToolExecutorOptions{})
	outcome, err := executor.Execute(context.Background(), tool.NewCall("call", "read_file", json.RawMessage(`{"path":"README.md"}`)))
	if err != nil || outcome.Status != ToolOutcomeFailed || outcome.Error == nil || outcome.Error.Kind != "invalid_prepared_call" || candidate.calls != 0 {
		t.Fatalf("unexpected mutated prepare outcome: %#v calls=%d err=%v", outcome, candidate.calls, err)
	}
}

func TestToolExecutorMapsPrepareCancellation(t *testing.T) {
	candidate := newOutcomeTool()
	candidate.prepare = func(context.Context, tool.Call) (tool.PreparedCall, error) {
		return tool.PreparedCall{}, context.Canceled
	}
	executor := newOutcomeExecutor(t, candidate, ToolExecutorOptions{})
	outcome, err := executor.Execute(context.Background(), tool.NewCall("call", "read_file", json.RawMessage(`{"path":"README.md"}`)))
	if err != nil || outcome.Status != ToolOutcomeInterrupted || outcome.Error == nil || outcome.Error.Kind != "interrupted" || candidate.calls != 0 {
		t.Fatalf("unexpected cancelled prepare outcome: %#v calls=%d err=%v", outcome, candidate.calls, err)
	}
}

func TestToolExecutorReturnsStructuredOutcomeForUnknownTool(t *testing.T) {
	executor, err := NewToolExecutor(tool.NewRegistry(), tool.NewArgumentValidator())
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := executor.Execute(context.Background(), tool.NewCall("call", "missing", json.RawMessage(`{}`)))
	if err != nil || outcome.Status != ToolOutcomeFailed || outcome.Error == nil || outcome.Error.Kind != "not_registered" {
		t.Fatalf("unexpected unknown-tool outcome: %#v err=%v", outcome, err)
	}
}

func TestToolExecutorRecordsHookFailureWithoutChangingSuccess(t *testing.T) {
	candidate := newOutcomeTool()
	hook := &outcomeHook{err: errors.New("diagnostic unavailable")}
	executor := newOutcomeExecutor(t, candidate, ToolExecutorOptions{Hooks: []PostExecutionHook{hook}})
	outcome, err := executor.Execute(context.Background(), tool.NewCall("call", "read_file", json.RawMessage(`{"path":"README.md"}`)))
	if err != nil || !outcome.Succeeded() || hook.calls != 1 {
		t.Fatalf("unexpected hook outcome: %#v err=%v calls=%d", outcome, err, hook.calls)
	}
	if got, ok := outcome.Metadata["hook_errors"].([]string); !ok || !reflect.DeepEqual(got, []string{"diagnostic unavailable"}) {
		t.Fatalf("hook error metadata missing: %#v", outcome.Metadata)
	}
}

func TestToolExecutorClassifiesPermissionRequiredWithoutExecutingPreparedCall(t *testing.T) {
	candidate := newOutcomeTool()
	candidate.spec.SideEffect = tool.SideEffectWrite
	candidate.prepare = func(context.Context, tool.Call) (tool.PreparedCall, error) {
		return tool.PreparedCall{}, outcomeKindError{kind: "permission_required"}
	}
	executor := newOutcomeExecutor(t, candidate, ToolExecutorOptions{})
	outcome, err := executor.Execute(context.Background(), tool.NewCall("call-permission", "read_file", json.RawMessage(`{"path":"outside"}`)))
	if err != nil || outcome.Status != ToolOutcomeDenied || outcome.Error == nil || outcome.Error.Kind != "permission_required" || candidate.calls != 0 {
		t.Fatalf("unexpected permission outcome: %#v err=%v execute_calls=%d", outcome, err, candidate.calls)
	}
}

func TestToolExecutorRunsPostHookForPartialFailedResult(t *testing.T) {
	failed := newOutcomeTool()
	failed.result = tool.Result{Text: "partial", Partial: true, Metadata: map[string]any{"operations": []any{map[string]any{"path": "a"}}}}
	failed.err = errors.New("commit failed")
	hook := &outcomeHook{}
	executor := newOutcomeExecutor(t, failed, ToolExecutorOptions{Hooks: []PostExecutionHook{hook}})
	outcome, err := executor.Execute(context.Background(), tool.NewCall("call", failed.spec.Name, json.RawMessage(`{"path":"README.md"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != ToolOutcomeFailed || !outcome.Partial || hook.calls != 1 || !hook.result.Partial {
		t.Fatalf("partial failure did not reach post hook: outcome=%#v hook=%#v", outcome, hook)
	}
}

func TestToolExecutorResultDoesNotShareMetadata(t *testing.T) {
	candidate := newOutcomeTool()
	candidate.result.Metadata = map[string]any{"key": "value"}
	executor := newOutcomeExecutor(t, candidate, ToolExecutorOptions{})
	outcome, err := executor.Execute(context.Background(), tool.NewCall("call", "read_file", json.RawMessage(`{"path":"README.md"}`)))
	if err != nil {
		t.Fatal(err)
	}
	outcome.Result.Metadata["key"] = "changed"
	if reflect.DeepEqual(candidate.result.Metadata, outcome.Result.Metadata) {
		t.Fatal("tool outcome shares result metadata with tool implementation")
	}
}

func newOutcomeTool() *outcomeTool {
	return &outcomeTool{spec: tool.Spec{
		Name: "read_file", Description: "Read a file",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, Concurrency: tool.ToolConcurrencyShared, Idempotent: true,
	}, result: tool.Result{Text: "file contents"}}
}

func newOutcomeExecutor(t *testing.T, candidate tool.Tool, options ToolExecutorOptions) *ToolExecutor {
	t.Helper()
	registry := tool.NewRegistry()
	if err := registry.Register(candidate); err != nil {
		t.Fatal(err)
	}
	executor, err := NewToolExecutorWithOptions(registry, tool.NewArgumentValidator(), options)
	if err != nil {
		t.Fatal(err)
	}
	return executor
}
