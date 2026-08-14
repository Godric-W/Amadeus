package tool

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Godric-W/Amadeus/internal/policy"
)

type executionServiceTestTool struct {
	name     string
	parallel bool
	target   *ContextTarget
	handle   func(context.Context, Invocation) (ToolResult, error)
	check    func(context.Context, Invocation) (PermissionEvaluation, error)
}

func (toolImpl *executionServiceTestTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        toolImpl.name,
		Description: "execution service test tool",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`),
		SideEffect:  SideEffectRead,
		Idempotent:  true,
	}
}

func (toolImpl *executionServiceTestTool) SupportsParallelToolCalls() bool { return toolImpl.parallel }

func (toolImpl *executionServiceTestTool) ValidateInput(ToolUseContext, Invocation) error { return nil }

func (toolImpl *executionServiceTestTool) Prepare(toolContext ToolUseContext, invocation Invocation) (PreparedToolUse, error) {
	evaluation := AllowPermission()
	if toolImpl.check != nil {
		var err error
		evaluation, err = toolImpl.check(toolContext.Context, invocation)
		if err != nil {
			return PreparedToolUse{}, err
		}
	}
	return PreparedToolUse{Invocation: invocation, Target: toolImpl.target, Permission: evaluation}, nil
}

type executionServiceScope struct {
	err     error
	targets []ContextTarget
}

func (scope *executionServiceScope) Ensure(_ context.Context, target ContextTarget) error {
	scope.targets = append(scope.targets, target)
	return scope.err
}

type executionServiceContextRefreshError struct{}

func (executionServiceContextRefreshError) Error() string { return "context refresh required" }
func (executionServiceContextRefreshError) ToolErrorKind() string {
	return "context_refresh_required"
}

func (toolImpl *executionServiceTestTool) Execute(toolContext ToolUseContext, prepared PreparedToolUse) (ToolResult, error) {
	if toolImpl.handle == nil {
		return ToolResult{Text: string(prepared.Invocation.Call.Payload)}, nil
	}
	return toolImpl.handle(toolContext.Context, prepared.Invocation)
}

type executionServiceApprovalPort struct {
	mutex    sync.Mutex
	decision policy.ApprovalDecision
	requests []policy.ApprovalRequest
	entered  chan struct{}
	release  <-chan struct{}
}

func (approvalPort *executionServiceApprovalPort) Decide(ctx context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	approvalPort.mutex.Lock()
	approvalPort.requests = append(approvalPort.requests, request.Clone())
	entered := approvalPort.entered
	approvalPort.entered = nil
	release := approvalPort.release
	approvalPort.mutex.Unlock()
	if entered != nil {
		close(entered)
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return policy.ApprovalDecision{}, ctx.Err()
		}
	}
	return approvalPort.decision, nil
}

func (approvalPort *executionServiceApprovalPort) count() int {
	approvalPort.mutex.Lock()
	defer approvalPort.mutex.Unlock()
	return len(approvalPort.requests)
}

type executionServicePermissionError struct{ message string }

func (err executionServicePermissionError) Error() string         { return err.message }
func (err executionServicePermissionError) ToolErrorKind() string { return "permission_required" }

type recordingLifecycleObserver struct {
	mutex      sync.Mutex
	started    []ToolCall
	completed  []ToolExecution
	startedErr error
	finishErr  error
}

func (observer *recordingLifecycleObserver) ToolCallStarted(_ context.Context, _ ToolSpec, call ToolCall) error {
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

func TestToolExecutionServiceRepairsValidatesAndExecutesOnce(t *testing.T) {
	var calls atomic.Int32
	toolImpl := &executionServiceTestTool{name: "read_test", parallel: true, handle: func(_ context.Context, invocation Invocation) (ToolResult, error) {
		calls.Add(1)
		if string(invocation.Call.Payload) != `{"value":"ok"}` {
			t.Fatalf("arguments were not normalized: %s", invocation.Call.Payload)
		}
		return ToolResult{Text: "done", Metadata: map[string]any{"path": "README.md"}}, nil
	}}
	service := newToolExecutionServiceForTest(t, ToolExecutionServiceOptions{}, toolImpl)
	execution, err := service.Execute(context.Background(), NewCall("call-1", toolImpl.name, json.RawMessage(`{"value":"ok",`)))
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || execution.Outcome.Status != ToolCallCompleted || execution.Output.CallID != "call-1" || execution.Output.ToolName != toolImpl.name || execution.Output.Text != "done" {
		t.Fatalf("unexpected execution: %#v calls=%d", execution, calls.Load())
	}
	execution.Output.Metadata["path"] = "changed"
	if execution.Outcome.Metadata["path"] != "README.md" {
		t.Fatalf("outcome metadata aliases output metadata: %#v", execution)
	}
}

func TestToolExecutionServiceReturnsModelVisibleLookupAndArgumentFailures(t *testing.T) {
	toolImpl := &executionServiceTestTool{name: "read_test", parallel: true}
	service := newToolExecutionServiceForTest(t, ToolExecutionServiceOptions{}, toolImpl)
	tests := []struct {
		name string
		call ToolCall
		kind string
	}{
		{name: "unknown", call: NewCall("unknown-1", "missing", json.RawMessage(`{}`)), kind: "not_registered"},
		{name: "invalid", call: NewCall("invalid-1", toolImpl.name, json.RawMessage(`{"value":12}`)), kind: "invalid_arguments"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			execution, err := service.Execute(context.Background(), test.call)
			if err != nil {
				t.Fatal(err)
			}
			if execution.Outcome.Status != ToolCallFailed || execution.Outcome.Error == nil || execution.Outcome.Error.Kind != test.kind || execution.Output.Text == "" {
				t.Fatalf("unexpected failure: %#v", execution)
			}
		})
	}
}

func TestToolExecutionServiceClassifiesDeniedPartialAndInterruptedCalls(t *testing.T) {
	tools := []ToolDefinition{
		&executionServiceTestTool{name: "denied", handle: func(context.Context, Invocation) (ToolResult, error) {
			return ToolResult{}, executionServicePermissionError{message: "outside writable roots"}
		}},
		&executionServiceTestTool{name: "partial", handle: func(context.Context, Invocation) (ToolResult, error) {
			return ToolResult{Text: "first operation applied", Partial: true}, errors.New("second operation failed")
		}},
		&executionServiceTestTool{name: "cancelled", handle: func(ctx context.Context, _ Invocation) (ToolResult, error) {
			return ToolResult{}, ctx.Err()
		}},
	}
	service := newToolExecutionServiceForTest(t, ToolExecutionServiceOptions{}, tools...)
	denied, _ := service.Execute(context.Background(), executionServiceCall("denied-1", "denied"))
	if denied.Outcome.Status != ToolCallDenied || denied.Outcome.Blocking || denied.Outcome.Error.Kind != "permission_required" {
		t.Fatalf("unexpected denied execution: %#v", denied)
	}
	partial, _ := service.Execute(context.Background(), executionServiceCall("partial-1", "partial"))
	if partial.Outcome.Status != ToolCallFailed || !partial.Output.Partial || partial.Output.Text != "first operation applied" {
		t.Fatalf("unexpected partial execution: %#v", partial)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	interrupted, _ := service.Execute(ctx, executionServiceCall("cancelled-1", "cancelled"))
	if interrupted.Outcome.Status != ToolCallInterrupted || interrupted.Outcome.Error == nil {
		t.Fatalf("unexpected interrupted execution: %#v", interrupted)
	}
}

func TestToolExecutionServiceRecordsNormalizedCallsBeforeAnySideEffect(t *testing.T) {
	var handled atomic.Int32
	toolImpl := &executionServiceTestTool{name: "write_test", parallel: false, handle: func(context.Context, Invocation) (ToolResult, error) {
		handled.Add(1)
		return ToolResult{Text: "changed"}, nil
	}}
	service := newToolExecutionServiceForTest(t, ToolExecutionServiceOptions{}, toolImpl)
	want := errors.New("rollout unavailable")
	_, err := service.ExecuteBatch(context.Background(), []ToolCall{
		NewCall("call-1", toolImpl.name, json.RawMessage(`{"value":"ok",`)),
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

func TestToolExecutionServiceChecksTargetScopeBeforePermissionAndExecution(t *testing.T) {
	var handled atomic.Int32
	target := &ContextTarget{Path: "/workspace/nested/file.go", Kind: ContextTargetFile, SideEffect: SideEffectWrite}
	toolImpl := &executionServiceTestTool{name: "write_test", target: target, handle: func(context.Context, Invocation) (ToolResult, error) {
		handled.Add(1)
		return ToolResult{Text: "changed"}, nil
	}}
	scope := &executionServiceScope{err: executionServiceContextRefreshError{}}
	service := newToolExecutionServiceForTest(t, ToolExecutionServiceOptions{ContextScope: scope}, toolImpl)
	execution, err := service.Execute(context.Background(), executionServiceCall("call-1", toolImpl.name))
	if err != nil {
		t.Fatal(err)
	}
	if handled.Load() != 0 || len(scope.targets) != 1 || scope.targets[0] != *target {
		t.Fatalf("scope gate did not fail closed: execution=%#v targets=%#v handled=%d", execution, scope.targets, handled.Load())
	}
	if execution.Outcome.Status != ToolCallFailed || execution.Outcome.Blocking || execution.Outcome.Error == nil || execution.Outcome.Error.Kind != "context_refresh_required" {
		t.Fatalf("context refresh outcome = %#v", execution.Outcome)
	}
}

func TestToolExecutionServiceInvalidCallDoesNotCancelValidSibling(t *testing.T) {
	var handled atomic.Int32
	toolImpl := &executionServiceTestTool{name: "read_test", parallel: true, handle: func(context.Context, Invocation) (ToolResult, error) {
		handled.Add(1)
		return ToolResult{Text: "ok"}, nil
	}}
	service := newToolExecutionServiceForTest(t, ToolExecutionServiceOptions{MaxParallel: 2}, toolImpl)
	var recorded []ToolCall
	executions, err := service.ExecuteBatch(context.Background(), []ToolCall{
		NewCall("bad", toolImpl.name, json.RawMessage(`{"value":`)),
		executionServiceCall("good", toolImpl.name),
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

func TestToolExecutionServiceAppliesPermissionDecisionsAndSessionGrant(t *testing.T) {
	permissions := policy.NewSessionPermissionContext()
	approvalPort := &executionServiceApprovalPort{decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalSession, Source: policy.ApprovalSourceUser, Reason: "trusted"}}
	coordinator, err := policy.NewApprovalCoordinator(approvalPort)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	grant := policy.ExternalGrant("test:shared")
	ask := func(invocation Invocation) PermissionEvaluation {
		request, requestErr := policy.NewApprovalRequestForPurpose(invocation.Call.ID, invocation.Call.Name, invocation.Call.Payload, policy.ApprovalPurposeExternal, policy.CommandRiskModerate, policy.ApprovalCause{Kind: policy.ApprovalCausePolicy, Code: "test_approval"})
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		request.PermissionKey = grant.Key
		return PermissionEvaluation{Decision: PermissionAsk, Request: &request, Grant: grant}
	}
	toolImpl := &executionServiceTestTool{name: "approved", parallel: false, check: func(_ context.Context, invocation Invocation) (PermissionEvaluation, error) {
		return ask(invocation), nil
	}, handle: func(context.Context, Invocation) (ToolResult, error) {
		calls.Add(1)
		return ToolResult{Text: "ok"}, nil
	}}
	service := newToolExecutionServiceForTest(t, ToolExecutionServiceOptions{Approvals: coordinator, Permissions: permissions}, toolImpl)
	for _, id := range []string{"first", "second"} {
		execution, executeErr := service.Execute(context.Background(), executionServiceCall(id, toolImpl.name))
		if executeErr != nil || execution.Outcome.Status != ToolCallCompleted {
			t.Fatalf("approved execution failed: %#v err=%v", execution, executeErr)
		}
	}
	if calls.Load() != 2 || approvalPort.count() != 1 || !permissions.Match(grant) {
		t.Fatalf("session grant was not reused: calls=%d approvals=%d grants=%d", calls.Load(), approvalPort.count(), permissions.GrantCount())
	}

	deniedCalls := atomic.Int32{}
	denied := &executionServiceTestTool{name: "denied_by_check", check: func(context.Context, Invocation) (PermissionEvaluation, error) {
		return PermissionEvaluation{Decision: PermissionDeny, Reason: "blocked"}, nil
	}, handle: func(context.Context, Invocation) (ToolResult, error) {
		deniedCalls.Add(1)
		return ToolResult{}, nil
	}}
	deniedService := newToolExecutionServiceForTest(t, ToolExecutionServiceOptions{}, denied)
	execution, err := deniedService.Execute(context.Background(), executionServiceCall("denied", denied.name))
	if err != nil || execution.Outcome.Status != ToolCallDenied || deniedCalls.Load() != 0 {
		t.Fatalf("permission deny reached Tool.Call: %#v calls=%d err=%v", execution, deniedCalls.Load(), err)
	}
}

func TestToolExecutionServicePublishesLifecycleAroundNormalizedExecution(t *testing.T) {
	observer := &recordingLifecycleObserver{}
	toolImpl := &executionServiceTestTool{name: "read_test", parallel: true}
	service := newToolExecutionServiceForTest(t, ToolExecutionServiceOptions{Observer: observer}, toolImpl)
	execution, err := service.Execute(context.Background(), NewCall("call-1", toolImpl.name, json.RawMessage(`{"value":"ok",`)))
	if err != nil {
		t.Fatal(err)
	}
	if len(observer.started) != 1 || string(observer.started[0].Payload) != `{"value":"ok"}` || len(observer.completed) != 1 || observer.completed[0].Call.ID != execution.Call.ID {
		t.Fatalf("unexpected lifecycle: started=%#v completed=%#v", observer.started, observer.completed)
	}
}

func TestToolExecutionServiceRejectsInvisibleHandlerAndPopulatesInvocationMetadata(t *testing.T) {
	var received Invocation
	toolImpl := &executionServiceTestTool{name: "conditional", parallel: true, handle: func(_ context.Context, invocation Invocation) (ToolResult, error) {
		received = invocation
		return ToolResult{Text: "ok"}, nil
	}}
	registry := NewRegistry()
	if err := registry.RegisterDefinitionWithRegistration(toolImpl, Registration{Exposure: ExposureConditional, Condition: "enabled"}); err != nil {
		t.Fatal(err)
	}
	hidden, err := NewToolExecutionService(registry, NewArgumentValidator(), ToolExecutionServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := hidden.Execute(context.Background(), executionServiceCall("hidden", toolImpl.name))
	if err != nil || execution.Outcome.Error == nil || execution.Outcome.Error.Kind != "not_registered" {
		t.Fatalf("invisible Tool executed: %#v err=%v", execution, err)
	}
	visible, err := NewToolExecutionService(registry, NewArgumentValidator(), ToolExecutionServiceOptions{Visibility: map[string]bool{"enabled": true}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithInvocationMetadata(context.Background(), InvocationMetadata{SessionID: "session-1", TurnID: "run-1", Source: ToolCallSourceUser})
	execution, err = visible.Execute(ctx, executionServiceCall("visible", toolImpl.name))
	if err != nil || execution.Outcome.Status != ToolCallCompleted {
		t.Fatalf("visible Tool failed: %#v err=%v", execution, err)
	}
	if received.SessionID != "session-1" || received.TurnID != "run-1" || received.Source != ToolCallSourceUser {
		t.Fatalf("Invocation metadata was not populated: %#v", received)
	}
}

func newToolExecutionServiceForTest(t *testing.T, options ToolExecutionServiceOptions, tools ...ToolDefinition) *ToolExecutionService {
	t.Helper()
	registry := NewRegistry()
	for _, candidate := range tools {
		if err := registry.RegisterDefinition(candidate); err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewToolExecutionService(registry, NewArgumentValidator(), options)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func executionServiceCall(id, name string) ToolCall {
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
