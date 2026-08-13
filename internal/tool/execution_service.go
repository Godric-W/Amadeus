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
	Observer    LifecycleObserver
	MaxParallel int
	Visibility  map[string]bool
	Approvals   *policy.ApprovalCoordinator
}

type ToolExecutionService struct {
	registry    *Registry
	validator   *ArgumentValidator
	observer    LifecycleObserver
	maxParallel int
	visibility  map[string]bool
	now         func() time.Time
	approvals   *policy.ApprovalCoordinator
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
	return &ToolExecutionService{
		registry: registry, validator: validator, observer: options.Observer,
		maxParallel: options.MaxParallel, visibility: cloneVisibility(options.Visibility),
		now: time.Now, approvals: options.Approvals,
	}, nil
}

func (service *ToolExecutionService) Execute(ctx context.Context, call ToolCall) (ToolExecution, error) {
	routed := service.prepareCall(call)
	if routed.failure != nil {
		return service.publishFailure(ctx, *routed.failure)
	}
	return service.executeCall(ctx, routed)
}

func (service *ToolExecutionService) ExecuteBatch(ctx context.Context, calls []ToolCall, recorder NormalizedCallRecorder) ([]ToolExecution, error) {
	routed := make([]executionCall, len(calls))
	normalized := make([]ToolCall, len(calls))
	for index, call := range calls {
		routed[index] = service.prepareCall(call)
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
		if !current.tool.SupportsParallelToolCalls() {
			execution, err := service.executeCall(ctx, current)
			if err != nil {
				return nil, err
			}
			completed = append(completed, indexedExecution{index: current.index, execution: execution})
			index++
			continue
		}
		end := index + 1
		for end < len(routed) && routed[end].failure == nil && routed[end].tool.SupportsParallelToolCalls() {
			end++
		}
		parallel, err := service.executeParallel(ctx, routed[index:end])
		if err != nil {
			return nil, err
		}
		completed = append(completed, parallel...)
		index = end
	}
	sort.Slice(completed, func(left, right int) bool { return completed[left].index < completed[right].index })
	executions := make([]ToolExecution, 0, len(completed))
	for _, item := range completed {
		executions = append(executions, item.execution)
	}
	return executions, nil
}

func (service *ToolExecutionService) SupportsParallelToolCalls(name string) bool {
	toolImpl, ok := service.registry.LookupVisible(name, service.visibility)
	return ok && toolImpl.SupportsParallelToolCalls()
}

type executionCall struct {
	index     int
	call      ToolCall
	spec      ToolSpec
	tool      Tool
	startedAt time.Time
	repairs   []ArgumentRepairKind
	failure   *ToolExecution
}

func (service *ToolExecutionService) prepareCall(call ToolCall) executionCall {
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
	toolImpl, ok := service.registry.LookupVisible(call.Name, service.visibility)
	if !ok {
		failure := service.failure(safeCall, "not_registered", fmt.Errorf("tool %q is not registered or visible", call.Name), startedAt)
		return executionCall{call: safeCall, startedAt: startedAt, failure: &failure}
	}
	spec := toolImpl.Spec()
	normalized, err := service.validator.Normalize(spec, call.Payload)
	if err != nil {
		failureCall := call.Clone()
		failureCall.Payload = json.RawMessage(`null`)
		failure := service.failure(failureCall, "invalid_arguments", err, startedAt)
		return executionCall{call: failureCall, spec: spec, tool: toolImpl, startedAt: startedAt, failure: &failure}
	}
	return executionCall{call: NewCall(call.ID, call.Name, normalized.Payload), spec: spec, tool: toolImpl, startedAt: startedAt, repairs: normalized.RepairKinds}
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
	permissionCtx, permissionErr := service.authorize(ctx, routed.tool, invocation)
	if permissionErr != nil {
		execution := service.complete(routed.call, Output{}, permissionErr, routed.startedAt)
		return service.publishCompleted(ctx, execution)
	}
	output, handleErr := routed.tool.Call(permissionCtx, invocation)
	execution := service.complete(routed.call, output, handleErr, routed.startedAt)
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

func (service *ToolExecutionService) authorize(ctx context.Context, candidate Tool, invocation Invocation) (context.Context, error) {
	checker, ok := candidate.(PermissionChecker)
	if !ok {
		return ctx, nil
	}
	check, err := checker.CheckPermissions(ctx, invocation)
	if err != nil {
		return ctx, fmt.Errorf("check tool permissions: %w", err)
	}
	if err := check.Validate(); err != nil {
		return ctx, fmt.Errorf("validate tool permissions: %w", err)
	}
	switch check.Decision {
	case PermissionAllow:
		if check.OnDecision != nil {
			if err := check.OnDecision(ctx, policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceGrant, Reason: "matching session permission grant"}); err != nil {
				return ctx, err
			}
		}
		return WithPermissionCheck(ctx, check), nil
	case PermissionDeny:
		if check.OnDecision != nil {
			if err := check.OnDecision(ctx, policy.ApprovalDecision{Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourcePolicy, Reason: check.Reason}); err != nil {
				return ctx, err
			}
		}
		return ctx, &PermissionDeniedError{ToolName: invocation.Call.Name, Reason: check.Reason}
	case PermissionAsk:
		if service.approvals == nil {
			return ctx, errors.New("tool permission requires approval coordinator")
		}
		if service.approvals.Permissions().Match(check.Grant) {
			decision := policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalSession, Source: policy.ApprovalSourceGrant, Reason: "matching session permission grant"}
			if check.OnDecision != nil {
				if err := check.OnDecision(ctx, decision); err != nil {
					return ctx, err
				}
			}
			return WithPermissionCheck(ctx, check), nil
		}
		decision, err := service.approvals.DecideForGrant(ctx, *check.Request, check.Grant)
		if err != nil {
			return ctx, err
		}
		if check.OnDecision != nil {
			if err := check.OnDecision(ctx, decision); err != nil {
				return ctx, err
			}
		}
		if !decision.Allowed() {
			return ctx, &PermissionDeniedError{ToolName: invocation.Call.Name, Reason: decision.Reason}
		}
		return WithPermissionCheck(ctx, check), nil
	default:
		return ctx, errors.New("unsupported tool permission decision")
	}
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

func (service *ToolExecutionService) complete(call ToolCall, output Output, handleErr error, startedAt time.Time) ToolExecution {
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

func (service *ToolExecutionService) failure(call ToolCall, kind string, err error, startedAt time.Time) ToolExecution {
	status, blocking := statusForError(err)
	return ToolExecution{
		Call: call.Clone(), Output: Output{CallID: call.ID, ToolName: call.Name, Text: err.Error()},
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
