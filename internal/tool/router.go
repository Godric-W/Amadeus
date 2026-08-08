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
)

type LifecycleObserver interface {
	ToolCallStarted(context.Context, Spec, ToolCall) error
	ToolCallCompleted(context.Context, ToolExecution) error
}

type NormalizedCallRecorder func(context.Context, []ToolCall) error

type RouterOptions struct {
	Observer    LifecycleObserver
	MaxParallel int
	Visibility  map[string]bool
}

type Router struct {
	registry    *Registry
	validator   *ArgumentValidator
	observer    LifecycleObserver
	maxParallel int
	visibility  map[string]bool
	gate        *runToolGate
	now         func() time.Time
}

func NewRouter(registry *Registry, validator *ArgumentValidator, options RouterOptions) (*Router, error) {
	if registry == nil {
		return nil, errors.New("tool router registry is nil")
	}
	if validator == nil {
		return nil, errors.New("tool router argument validator is nil")
	}
	if options.MaxParallel <= 0 {
		options.MaxParallel = 1
	}
	return &Router{
		registry: registry, validator: validator, observer: options.Observer,
		maxParallel: options.MaxParallel, visibility: cloneVisibility(options.Visibility),
		gate: newRunToolGate(), now: time.Now,
	}, nil
}

func (router *Router) Execute(ctx context.Context, call ToolCall) (ToolExecution, error) {
	routed := router.route(call)
	if routed.failure != nil {
		return router.publishFailure(ctx, *routed.failure)
	}
	return router.executeRouted(ctx, routed)
}

func (router *Router) ExecuteBatch(ctx context.Context, calls []ToolCall, recorder NormalizedCallRecorder) ([]ToolExecution, error) {
	routed := make([]routedCall, len(calls))
	normalized := make([]ToolCall, len(calls))
	for index, call := range calls {
		routed[index] = router.route(call)
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
			execution, err := router.publishFailure(ctx, *current.failure)
			if err != nil {
				return nil, err
			}
			completed = append(completed, indexedExecution{index: current.index, execution: execution})
			index++
			continue
		}
		if !current.handler.SupportsParallelToolCalls() {
			execution, err := router.executeRouted(ctx, current)
			if err != nil {
				return nil, err
			}
			completed = append(completed, indexedExecution{index: current.index, execution: execution})
			index++
			continue
		}
		end := index + 1
		for end < len(routed) && routed[end].failure == nil && routed[end].handler.SupportsParallelToolCalls() {
			end++
		}
		parallel, err := router.executeParallel(ctx, routed[index:end])
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

func (router *Router) SupportsParallelToolCalls(name string) bool {
	handler, ok := router.registry.LookupVisible(name, router.visibility)
	return ok && handler.SupportsParallelToolCalls()
}

type routedCall struct {
	index     int
	call      ToolCall
	spec      Spec
	handler   Handler
	startedAt time.Time
	repairs   []ArgumentRepairKind
	failure   *ToolExecution
}

func (router *Router) route(call ToolCall) routedCall {
	startedAt := router.now()
	safeCall := protocolSafeCall(call)
	if strings.TrimSpace(call.ID) == "" {
		failure := router.failure(safeCall, "invalid_call", errors.New("tool call ID is empty"), startedAt)
		return routedCall{call: safeCall, startedAt: startedAt, failure: &failure}
	}
	if strings.TrimSpace(call.Name) == "" {
		failure := router.failure(safeCall, "invalid_call", errors.New("tool call name is empty"), startedAt)
		return routedCall{call: safeCall, startedAt: startedAt, failure: &failure}
	}
	handler, ok := router.registry.LookupVisible(call.Name, router.visibility)
	if !ok {
		failure := router.failure(safeCall, "not_registered", fmt.Errorf("tool %q is not registered or visible", call.Name), startedAt)
		return routedCall{call: safeCall, startedAt: startedAt, failure: &failure}
	}
	spec := handler.Spec()
	normalized, err := router.validator.Normalize(spec, call.Payload)
	if err != nil {
		failureCall := call.Clone()
		failureCall.Payload = json.RawMessage(`null`)
		failure := router.failure(failureCall, "invalid_arguments", err, startedAt)
		return routedCall{call: failureCall, spec: spec, handler: handler, startedAt: startedAt, failure: &failure}
	}
	return routedCall{call: NewCall(call.ID, call.Name, normalized.Payload), spec: spec, handler: handler, startedAt: startedAt, repairs: normalized.RepairKinds}
}

func protocolSafeCall(call ToolCall) ToolCall {
	call = call.Clone()
	if !json.Valid(call.Payload) {
		call.Payload = json.RawMessage(`null`)
	}
	return call
}

func (router *Router) executeRouted(ctx context.Context, routed routedCall) (ToolExecution, error) {
	if router.observer != nil {
		if err := router.observer.ToolCallStarted(ctx, routed.spec, routed.call); err != nil {
			return ToolExecution{}, fmt.Errorf("publish tool call started: %w", err)
		}
	}

	unlock, err := router.gate.acquire(ctx, routed.handler.SupportsParallelToolCalls())
	if err != nil {
		execution := router.complete(routed.call, Output{}, err, routed.startedAt)
		return router.publishCompleted(ctx, execution)
	}
	metadata := InvocationMetadataFromContext(ctx)
	if metadata.Source == "" {
		metadata.Source = ToolCallSourceModel
	}
	output, handleErr := routed.handler.Handle(ctx, Invocation{
		SessionID: metadata.SessionID, RunID: metadata.RunID,
		Call: routed.call, Source: metadata.Source,
	})
	unlock()
	execution := router.complete(routed.call, output, handleErr, routed.startedAt)
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
	return router.publishCompleted(ctx, execution)
}

func (router *Router) publishFailure(ctx context.Context, execution ToolExecution) (ToolExecution, error) {
	return router.publishCompleted(ctx, execution)
}

func (router *Router) publishCompleted(ctx context.Context, execution ToolExecution) (ToolExecution, error) {
	if router.observer != nil {
		if err := router.observer.ToolCallCompleted(ctx, execution); err != nil {
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

func (router *Router) executeParallel(ctx context.Context, calls []routedCall) ([]indexedExecution, error) {
	workerCount := router.maxParallel
	if workerCount > len(calls) {
		workerCount = len(calls)
	}
	jobs := make(chan routedCall)
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
				execution, err := router.executeRouted(ctx, current)
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

func (router *Router) complete(call ToolCall, output Output, handleErr error, startedAt time.Time) ToolExecution {
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
		Outcome: ToolCallOutcome{Status: status, Error: toolError, Blocking: blocking, Duration: router.durationSince(startedAt), Metadata: cloneMetadata(output.Metadata)},
	}
}

func (router *Router) failure(call ToolCall, kind string, err error, startedAt time.Time) ToolExecution {
	status, blocking := statusForError(err)
	return ToolExecution{
		Call: call.Clone(), Output: Output{CallID: call.ID, ToolName: call.Name, Text: err.Error()},
		Outcome: ToolCallOutcome{Status: status, Error: &ToolError{Kind: kind, Message: err.Error()}, Blocking: blocking, Duration: router.durationSince(startedAt)},
	}
}

func (router *Router) durationSince(startedAt time.Time) time.Duration {
	finishedAt := router.now()
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

type runToolGate struct {
	mutex          sync.Mutex
	readers        int
	writer         bool
	waitingWriters int
	changed        chan struct{}
}

func newRunToolGate() *runToolGate {
	return &runToolGate{changed: make(chan struct{})}
}

func (gate *runToolGate) acquire(ctx context.Context, parallel bool) (func(), error) {
	if ctx == nil {
		return nil, errors.New("tool gate context is nil")
	}
	waitingWriter := false
	for {
		gate.mutex.Lock()
		if parallel {
			if !gate.writer && gate.waitingWriters == 0 {
				gate.readers++
				gate.mutex.Unlock()
				return gate.releaseReader, nil
			}
		} else {
			if !waitingWriter {
				gate.waitingWriters++
				waitingWriter = true
			}
			if !gate.writer && gate.readers == 0 {
				gate.waitingWriters--
				gate.writer = true
				gate.mutex.Unlock()
				return gate.releaseWriter, nil
			}
		}
		changed := gate.changed
		gate.mutex.Unlock()

		select {
		case <-ctx.Done():
			if waitingWriter {
				gate.mutex.Lock()
				gate.waitingWriters--
				gate.signalLocked()
				gate.mutex.Unlock()
			}
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

func (gate *runToolGate) releaseReader() {
	gate.mutex.Lock()
	gate.readers--
	gate.signalLocked()
	gate.mutex.Unlock()
}

func (gate *runToolGate) releaseWriter() {
	gate.mutex.Lock()
	gate.writer = false
	gate.signalLocked()
	gate.mutex.Unlock()
}

func (gate *runToolGate) signalLocked() {
	close(gate.changed)
	gate.changed = make(chan struct{})
}
