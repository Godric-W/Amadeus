package tool

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

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
