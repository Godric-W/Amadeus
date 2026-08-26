package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/policy"
)

type LifecycleObserver interface {
	ToolCallStarted(context.Context, ToolSpec, ToolCall) error
	ToolCallCompleted(context.Context, ToolExecution) error
}

type NormalizedCallRecorder func(context.Context, []ToolCall) error

type ToolExecutionServiceOptions struct {
	Observer       LifecycleObserver
	MaxParallel    int
	Visibility     map[string]bool
	Approvals      *policy.ApprovalCoordinator
	Permissions    *policy.SessionPermissionContext
	FileReadState  *FileReadStateStore
	TargetObserver TargetObserver
	Interactions   UserInputRequester
}

// ExecutionScope binds request-scoped capabilities to one model step without
// rebuilding the session-scoped registry, validator, permission state, or file
// read state.
type ExecutionScope struct {
	Observer LifecycleObserver
	Router   *ToolRouter
}

type ToolExecutionService struct {
	registry       *Registry
	validator      *ArgumentValidator
	observer       LifecycleObserver
	maxParallel    int
	visibility     map[string]bool
	now            func() time.Time
	permissions    *PermissionService
	fileReadState  *FileReadStateStore
	targetObserver TargetObserver
	interactions   UserInputRequester
}

func NewToolExecutionService(registry *Registry, validator *ArgumentValidator, options ToolExecutionServiceOptions) (*ToolExecutionService, error) {
	if registry == nil {
		return nil, errors.New("tool service registry is nil")
	}
	if validator == nil {
		return nil, errors.New("tool service argument validator is nil")
	}
	if options.MaxParallel <= 0 {
		options.MaxParallel = 1
	}
	if options.FileReadState == nil {
		options.FileReadState = NewFileReadStateStore()
	}
	return &ToolExecutionService{
		registry: registry, validator: validator, observer: options.Observer,
		maxParallel: options.MaxParallel, visibility: cloneVisibility(options.Visibility),
		now: time.Now, permissions: NewPermissionService(options.Permissions, options.Approvals),
		fileReadState: options.FileReadState, targetObserver: options.TargetObserver, interactions: options.Interactions,
	}, nil
}

func (service *ToolExecutionService) Execute(ctx context.Context, call ToolCall) (ToolExecution, error) {
	routed := service.prepareCall(service.currentRouter(), call, "not_registered")
	if routed.failure != nil {
		return service.publishFailure(ctx, *routed.failure)
	}
	return service.executeCall(ctx, routed)
}

type executionCall struct {
	index     int
	call      ToolCall
	spec      ToolSpec
	tool      ToolDefinition
	parallel  bool
	startedAt time.Time
	repairs   []ArgumentRepairKind
	failure   *ToolExecution
}

func (service *ToolExecutionService) prepareCall(router ToolRouter, call ToolCall, unavailableKind string) executionCall {
	startedAt := service.now()
	safeCall := protocolSafeCall(call)
	if strings.TrimSpace(call.ID) == "" {
		failure := service.failure(safeCall, "invalid_call", errors.New("tool call ID is empty"), startedAt)
		return executionCall{call: safeCall, startedAt: startedAt, failure: &failure}
	}
	if strings.TrimSpace(call.Name) == "" {
		failure := service.failure(safeCall, "invalid_call", errors.New("tool call name is empty"), startedAt)
		return executionCall{call: safeCall, startedAt: startedAt, failure: &failure}
	}
	route, ok := router.resolve(call.Name)
	if !ok {
		message := fmt.Errorf("tool %q is not registered or visible", call.Name)
		if unavailableKind == "tool_not_available" {
			message = fmt.Errorf("tool %q is not available in this model step", call.Name)
		}
		failure := service.failure(safeCall, unavailableKind, message, startedAt)
		return executionCall{call: safeCall, startedAt: startedAt, failure: &failure}
	}
	spec := route.spec.Clone()
	normalized, err := service.validator.Normalize(spec, call.Payload)
	if err != nil {
		failureCall := call.Clone()
		failureCall.Payload = json.RawMessage(`null`)
		failure := service.failure(failureCall, "invalid_arguments", err, startedAt)
		return executionCall{call: failureCall, spec: spec, tool: route.definition, parallel: route.parallel, startedAt: startedAt, failure: &failure}
	}
	return executionCall{call: NewCall(call.ID, call.Name, normalized.Payload), spec: spec, tool: route.definition, parallel: route.parallel, startedAt: startedAt, repairs: normalized.RepairKinds}
}

func (service *ToolExecutionService) currentRouter() ToolRouter {
	if service == nil || service.registry == nil {
		return ToolRouter{}
	}
	return service.registry.SnapshotRouter(service.visibility, RequestSnapshot{}, nil)
}

func protocolSafeCall(call ToolCall) ToolCall {
	call = call.Clone()
	if !json.Valid(call.Payload) {
		call.Payload = json.RawMessage(`null`)
	}
	return call
}

func (service *ToolExecutionService) executeCall(ctx context.Context, routed executionCall) (ToolExecution, error) {
	if service.observer != nil {
		if err := service.observer.ToolCallStarted(ctx, routed.spec, routed.call); err != nil {
			return ToolExecution{}, fmt.Errorf("publish tool call started: %w", err)
		}
	}

	metadata := InvocationMetadataFromContext(ctx)
	if metadata.Source == "" {
		metadata.Source = ToolCallSourceModel
	}
	invocation := Invocation{
		SessionID: metadata.SessionID, ThreadID: metadata.ThreadID, TurnID: metadata.TurnID,
		Call: routed.call, Source: metadata.Source,
	}
	toolContext := ToolUseContext{
		Context: ctx, Invocation: invocation,
		Permissions:   service.permissions.permissions,
		FileReadState: service.fileReadState,
		Interactions:  service.interactions,
	}
	if snapshot, ok := RequestSnapshotFromContext(ctx); ok {
		toolContext.Snapshot = snapshot
	}
	if err := toolContext.Validate(); err != nil {
		execution := service.complete(routed.call, ToolResult{}, err, routed.startedAt)
		return service.publishCompleted(ctx, execution)
	}
	if err := routed.tool.ValidateInput(toolContext, invocation); err != nil {
		execution := service.complete(routed.call, ToolResult{}, &phaseError{kind: "validation_failed", err: err}, routed.startedAt)
		return service.publishCompleted(ctx, execution)
	}
	prepared, err := routed.tool.Prepare(toolContext, invocation)
	if err != nil {
		execution := service.complete(routed.call, ToolResult{}, &phaseError{kind: "preparation_failed", err: err}, routed.startedAt)
		return service.publishCompleted(ctx, execution)
	}
	if err := prepared.Validate(); err != nil {
		execution := service.complete(routed.call, ToolResult{}, &phaseError{kind: "preparation_failed", err: err}, routed.startedAt)
		return service.publishCompleted(ctx, execution)
	}
	if prepared.Invocation.Call.ID != invocation.Call.ID || prepared.Invocation.Call.Name != invocation.Call.Name {
		execution := service.complete(routed.call, ToolResult{}, &phaseError{kind: "preparation_failed", err: errors.New("prepared invocation does not match routed invocation")}, routed.startedAt)
		return service.publishCompleted(ctx, execution)
	}
	if prepared.Target != nil && service.targetObserver != nil {
		if err := service.targetObserver.ObserveTarget(ctx, *prepared.Target, toolContext.Snapshot); err != nil {
			execution := service.complete(routed.call, ToolResult{}, err, routed.startedAt)
			return service.publishCompleted(ctx, execution)
		}
	}
	if err := service.permissions.Evaluate(ctx, invocation.Call.Name, prepared.Permission); err != nil {
		execution := service.complete(routed.call, ToolResult{}, err, routed.startedAt)
		return service.publishCompleted(ctx, execution)
	}
	result, handleErr := routed.tool.Execute(toolContext, prepared)
	execution := service.complete(routed.call, result, handleErr, routed.startedAt)
	if len(routed.repairs) > 0 {
		if execution.Outcome.Metadata == nil {
			execution.Outcome.Metadata = make(map[string]any)
		}
		repairs := make([]string, len(routed.repairs))
		for index, repair := range routed.repairs {
			repairs[index] = string(repair)
		}
		execution.Outcome.Metadata["argument_repairs"] = repairs
	}
	return service.publishCompleted(ctx, execution)
}

func (service *ToolExecutionService) publishFailure(ctx context.Context, execution ToolExecution) (ToolExecution, error) {
	return service.publishCompleted(ctx, execution)
}

func (service *ToolExecutionService) publishCompleted(ctx context.Context, execution ToolExecution) (ToolExecution, error) {
	if service.observer != nil {
		if err := service.observer.ToolCallCompleted(ctx, execution); err != nil {
			return ToolExecution{}, fmt.Errorf("publish tool call completed: %w", err)
		}
	}
	return execution, nil
}

func cloneVisibility(value map[string]bool) map[string]bool {
	if value == nil {
		return nil
	}
	cloned := make(map[string]bool, len(value))
	for key, visible := range value {
		cloned[key] = visible
	}
	return cloned
}
