package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
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
		fileReadState: options.FileReadState, targetObserver: options.TargetObserver,
	}, nil
}

func (service *ToolExecutionService) Execute(ctx context.Context, call ToolCall) (ToolExecution, error) {
	routed := service.prepareCall(service.currentRouter(), call, "not_registered")
	if routed.failure != nil {
		return service.publishFailure(ctx, *routed.failure)
	}
	return service.executeCall(ctx, routed)
}

func (service *ToolExecutionService) ExecuteBatch(ctx context.Context, calls []ToolCall, recorder NormalizedCallRecorder) ([]ToolExecution, error) {
	return service.executeBatch(ctx, calls, recorder, service.currentRouter(), "not_registered")
}

func (service *ToolExecutionService) executeBatch(ctx context.Context, calls []ToolCall, recorder NormalizedCallRecorder, router ToolRouter, unavailableKind string) ([]ToolExecution, error) {
	routed := make([]executionCall, len(calls))
	normalized := make([]ToolCall, len(calls))
	for index, call := range calls {
		routed[index] = service.prepareCall(router, call, unavailableKind)
		routed[index].index = index
		normalized[index] = routed[index].call.Clone()
	}
	if recorder != nil {
		if err := recorder(ctx, normalized); err != nil {
			return nil, fmt.Errorf("record normalized Tool Calls: %w", err)
		}
	}

	completed := make([]indexedExecution, 0, len(routed))
	for index := 0; index < len(routed); {
		if err := ctx.Err(); err != nil {
			break
		}
		current := routed[index]
		if current.failure != nil {
			execution, err := service.publishFailure(ctx, *current.failure)
			if err != nil {
				return nil, err
			}
			completed = append(completed, indexedExecution{index: current.index, execution: execution})
			index++
			continue
		}
		if !current.parallel {
			execution, err := service.executeCall(ctx, current)
			if err != nil {
				return nil, err
			}
			completed = append(completed, indexedExecution{index: current.index, execution: execution})
			index++
			continue
		}
		end := index + 1
		for end < len(routed) && routed[end].failure == nil && routed[end].parallel {
			end++
		}
		parallel, err := service.executeParallel(ctx, routed[index:end])
		if err != nil {
			return nil, err
		}
		completed = append(completed, parallel...)
		index = end
	}
	if cancelErr := ctx.Err(); cancelErr != nil && len(completed) < len(routed) {
		seen := make(map[int]struct{}, len(completed))
		for _, item := range completed {
			seen[item.index] = struct{}{}
		}
		for _, pending := range routed {
			if _, ok := seen[pending.index]; ok {
				continue
			}
			execution := service.failure(pending.call, "interrupted", cancelErr, pending.startedAt)
			published, err := service.publishFailure(context.WithoutCancel(ctx), execution)
			if err != nil {
				return nil, err
			}
			completed = append(completed, indexedExecution{index: pending.index, execution: published})
		}
	}
	sort.Slice(completed, func(left, right int) bool { return completed[left].index < completed[right].index })
	executions := make([]ToolExecution, 0, len(completed))
	for _, item := range completed {
		executions = append(executions, item.execution)
	}
	return executions, nil
}

func (service *ToolExecutionService) ExecuteBatchScoped(ctx context.Context, calls []ToolCall, recorder NormalizedCallRecorder, scope ExecutionScope) ([]ToolExecution, error) {
	if service == nil {
		return nil, errors.New("tool execution service is nil")
	}
	bound := *service
	orderedObserver := &orderedBatchObserver{delegate: scope.Observer}
	bound.observer = orderedObserver
	if scope.Router == nil || !scope.Router.isConfigured() {
		return nil, errors.New("tool execution scope has no frozen router")
	}
	ctx = WithRequestSnapshot(ctx, scope.Router.RequestSnapshot())
	executions, err := bound.executeBatch(ctx, calls, recorder, *scope.Router, "tool_not_available")
	if err != nil {
		return nil, err
	}
	if scope.Observer != nil {
		completionCtx := context.WithoutCancel(ctx)
		for _, execution := range executions {
			if err := scope.Observer.ToolCallCompleted(completionCtx, execution); err != nil {
				return nil, fmt.Errorf("publish ordered tool completion: %w", err)
			}
		}
	}
	return executions, nil
}

type orderedBatchObserver struct {
	delegate LifecycleObserver
}

func (observer *orderedBatchObserver) ToolCallStarted(ctx context.Context, spec ToolSpec, call ToolCall) error {
	if observer == nil || observer.delegate == nil {
		return nil
	}
	return observer.delegate.ToolCallStarted(ctx, spec, call)
}

func (*orderedBatchObserver) ToolCallCompleted(context.Context, ToolExecution) error { return nil }

func (service *ToolExecutionService) SupportsParallelToolCalls(name string) bool {
	return service.currentRouter().SupportsParallelToolCalls(name)
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
		SessionID: metadata.SessionID, TurnID: metadata.TurnID,
		Call: routed.call, Source: metadata.Source,
	}
	toolContext := ToolUseContext{
		Context: ctx, Invocation: invocation,
		Permissions:   service.permissions.permissions,
		FileReadState: service.fileReadState,
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

type indexedExecution struct {
	index     int
	execution ToolExecution
}

type indexedExecutionError struct {
	index int
	err   error
}

func (service *ToolExecutionService) executeParallel(ctx context.Context, calls []executionCall) ([]indexedExecution, error) {
	workerCount := service.maxParallel
	if workerCount > len(calls) {
		workerCount = len(calls)
	}
	jobs := make(chan executionCall)
	results := make(chan indexedExecution, len(calls))
	errorsFound := make(chan indexedExecutionError, len(calls))
	var workers sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for current := range jobs {
				if ctx.Err() != nil {
					continue
				}
				execution, err := service.executeCall(ctx, current)
				if err != nil {
					errorsFound <- indexedExecutionError{index: current.index, err: err}
					continue
				}
				results <- indexedExecution{index: current.index, execution: execution}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, call := range calls {
			select {
			case jobs <- call:
			case <-ctx.Done():
				return
			}
		}
	}()
	workers.Wait()
	close(results)
	close(errorsFound)
	completed := make([]indexedExecution, 0, len(calls))
	for result := range results {
		completed = append(completed, result)
	}
	internalErrors := make([]indexedExecutionError, 0, len(errorsFound))
	for executionError := range errorsFound {
		internalErrors = append(internalErrors, executionError)
	}
	if len(internalErrors) > 0 {
		sort.Slice(internalErrors, func(left, right int) bool { return internalErrors[left].index < internalErrors[right].index })
		return nil, internalErrors[0].err
	}
	return completed, nil
}

func (service *ToolExecutionService) complete(call ToolCall, output ToolResult, handleErr error, startedAt time.Time) ToolExecution {
	output.CallID = call.ID
	output.ToolName = call.Name
	status := ToolCallCompleted
	var toolError *ToolError
	blocking := false
	if handleErr != nil {
		status, blocking = statusForError(handleErr)
		kind := "execution_failed"
		var kindProvider ErrorKindProvider
		if errors.As(handleErr, &kindProvider) && strings.TrimSpace(kindProvider.ToolErrorKind()) != "" {
			kind = strings.TrimSpace(kindProvider.ToolErrorKind())
		}
		toolError = &ToolError{Kind: kind, Message: handleErr.Error()}
		if strings.TrimSpace(output.Text) == "" {
			output.Text = handleErr.Error()
		}
	}
	return ToolExecution{
		Call: call.Clone(), Output: output.Clone(),
		Outcome: ToolCallOutcome{Status: status, Error: toolError, Blocking: blocking, Duration: service.durationSince(startedAt), Metadata: cloneMetadata(output.Metadata)},
	}
}

type phaseError struct {
	kind string
	err  error
}

func (err *phaseError) Error() string         { return err.err.Error() }
func (err *phaseError) Unwrap() error         { return err.err }
func (err *phaseError) ToolErrorKind() string { return err.kind }

func (service *ToolExecutionService) failure(call ToolCall, kind string, err error, startedAt time.Time) ToolExecution {
	status, blocking := statusForError(err)
	return ToolExecution{
		Call: call.Clone(), Output: ToolResult{CallID: call.ID, ToolName: call.Name, Text: err.Error()},
		Outcome: ToolCallOutcome{Status: status, Error: &ToolError{Kind: kind, Message: err.Error()}, Blocking: blocking, Duration: service.durationSince(startedAt)},
	}
}

func (service *ToolExecutionService) durationSince(startedAt time.Time) time.Duration {
	finishedAt := service.now()
	if finishedAt.Before(startedAt) {
		return 0
	}
	return finishedAt.Sub(startedAt)
}

func statusForError(err error) (ToolCallStatus, bool) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ToolCallInterrupted, false
	}
	var kindProvider ErrorKindProvider
	if errors.As(err, &kindProvider) {
		switch strings.TrimSpace(kindProvider.ToolErrorKind()) {
		case "permission_required", "permission_denied", "path_denied", "symlink_escape", "approval_denied":
			return ToolCallDenied, false
		}
	}
	return ToolCallFailed, false
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
