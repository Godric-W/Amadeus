package tool

import (
	"context"
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

type RouterOptions struct {
	Observer    LifecycleObserver
	MaxParallel int
}

type Router struct {
	registry    *Registry
	validator   *ArgumentValidator
	observer    LifecycleObserver
	maxParallel int
	gate        sync.RWMutex
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
	return &Router{registry: registry, validator: validator, observer: options.Observer, maxParallel: options.MaxParallel, now: time.Now}, nil
}

func (router *Router) Execute(ctx context.Context, call ToolCall) (ToolExecution, error) {
	startedAt := router.now()
	if strings.TrimSpace(call.ID) == "" {
		return router.failure(call, "invalid_call", errors.New("tool call ID is empty"), startedAt), nil
	}
	if strings.TrimSpace(call.Name) == "" {
		return router.failure(call, "invalid_call", errors.New("tool call name is empty"), startedAt), nil
	}
	handler, ok := router.registry.Lookup(call.Name)
	if !ok {
		return router.failure(call, "not_registered", fmt.Errorf("tool %q is not registered", call.Name), startedAt), nil
	}
	spec := handler.Spec()
	normalized, err := router.validator.Validate(spec, call.Arguments)
	if err != nil {
		return router.failure(call, "invalid_arguments", err, startedAt), nil
	}
	normalizedCall := NewCall(call.ID, call.Name, normalized)
	if router.observer != nil {
		if err := router.observer.ToolCallStarted(ctx, spec, normalizedCall); err != nil {
			return ToolExecution{}, fmt.Errorf("publish tool call started: %w", err)
		}
	}

	unlock := router.acquire(handler.SupportsParallelToolCalls())
	output, handleErr := handler.Handle(ctx, Invocation{Call: normalizedCall, Source: ToolCallSourceModel})
	unlock()

	execution := router.complete(normalizedCall, output, handleErr, startedAt)
	if router.observer != nil {
		if err := router.observer.ToolCallCompleted(ctx, execution); err != nil {
			return ToolExecution{}, fmt.Errorf("publish tool call completed: %w", err)
		}
	}
	return execution, nil
}

func (router *Router) ExecuteBatch(ctx context.Context, calls []ToolCall) ([]ToolExecution, error) {
	completed := make([]indexedExecution, 0, len(calls))
	for index := 0; index < len(calls); {
		if err := ctx.Err(); err != nil {
			break
		}
		if !router.supportsParallel(calls[index]) {
			execution, err := router.Execute(ctx, calls[index])
			if err != nil {
				return nil, err
			}
			completed = append(completed, indexedExecution{index: index, execution: execution})
			index++
			continue
		}
		end := index + 1
		for end < len(calls) && router.supportsParallel(calls[end]) {
			end++
		}
		parallel, err := router.executeParallel(ctx, calls[index:end], index)
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
	handler, ok := router.registry.Lookup(name)
	return ok && handler.SupportsParallelToolCalls()
}

func (router *Router) supportsParallel(call ToolCall) bool {
	return router.SupportsParallelToolCalls(call.Name)
}

func (router *Router) acquire(parallel bool) func() {
	if parallel {
		router.gate.RLock()
		return router.gate.RUnlock
	}
	router.gate.Lock()
	return router.gate.Unlock
}

type indexedExecution struct {
	index     int
	execution ToolExecution
}

type indexedExecutionError struct {
	index int
	err   error
}

func (router *Router) executeParallel(ctx context.Context, calls []ToolCall, offset int) ([]indexedExecution, error) {
	workerCount := router.maxParallel
	if workerCount > len(calls) {
		workerCount = len(calls)
	}
	type job struct {
		index int
		call  ToolCall
	}
	jobs := make(chan job)
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
				execution, err := router.Execute(ctx, current.call)
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
		for index, call := range calls {
			select {
			case jobs <- job{index: offset + index, call: call}:
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
		Outcome: ToolCallOutcome{Status: status, Error: toolError, Blocking: blocking, Duration: router.now().Sub(startedAt), Metadata: cloneMetadata(output.Metadata)},
	}
}

func (router *Router) failure(call ToolCall, kind string, err error, startedAt time.Time) ToolExecution {
	status, blocking := statusForError(err)
	return ToolExecution{
		Call: call.Clone(), Output: Output{CallID: call.ID, ToolName: call.Name, Text: err.Error()},
		Outcome: ToolCallOutcome{Status: status, Error: &ToolError{Kind: kind, Message: err.Error()}, Blocking: blocking, Duration: router.now().Sub(startedAt)},
	}
}

func statusForError(err error) (ToolCallStatus, bool) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ToolCallInterrupted, false
	}
	var kindProvider ErrorKindProvider
	if errors.As(err, &kindProvider) {
		switch strings.TrimSpace(kindProvider.ToolErrorKind()) {
		case "permission_required", "permission_denied", "path_denied", "symlink_escape", "approval_denied":
			return ToolCallDenied, true
		}
	}
	return ToolCallFailed, false
}
