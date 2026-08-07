package react

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type indexedToolExecution struct {
	index   int
	outcome ToolOutcome
}

type indexedToolError struct {
	index int
	err   error
}

type toolExecutionGate struct {
	executor    CallExecutor
	maxParallel int
}

func newToolExecutionGate(executor CallExecutor, maxParallel int) (*toolExecutionGate, error) {
	if executor == nil {
		return nil, errors.New("tool execution gate call executor is nil")
	}
	if maxParallel <= 0 {
		return nil, errors.New("tool execution gate max parallelism must be greater than zero")
	}
	return &toolExecutionGate{executor: executor, maxParallel: maxParallel}, nil
}

func (gate *toolExecutionGate) Execute(ctx context.Context, calls []tool.Call, specs []tool.Spec) ([]indexedToolExecution, error) {
	indexedSpecs, err := indexToolSpecs(specs)
	if err != nil {
		return nil, err
	}
	completed := make([]indexedToolExecution, 0, len(calls))
	for index := 0; index < len(calls); {
		if err := ctx.Err(); err != nil {
			break
		}
		if toolConcurrency(calls[index], indexedSpecs) == tool.ToolConcurrencyExclusive {
			outcome, err := gate.executor.Execute(ctx, calls[index])
			if err != nil {
				return nil, err
			}
			completed = append(completed, indexedToolExecution{index: index, outcome: outcome})
			index++
			continue
		}
		end := index + 1
		for end < len(calls) && toolConcurrency(calls[end], indexedSpecs) == tool.ToolConcurrencyShared {
			end++
		}
		shared, err := gate.executeShared(ctx, calls[index:end], index)
		if err != nil {
			return nil, err
		}
		completed = append(completed, shared...)
		index = end
	}
	sort.Slice(completed, func(left, right int) bool { return completed[left].index < completed[right].index })
	return completed, nil
}

func (gate *toolExecutionGate) executeShared(ctx context.Context, calls []tool.Call, offset int) ([]indexedToolExecution, error) {
	type job struct {
		index int
		call  tool.Call
	}
	workerCount := gate.maxParallel
	if workerCount > len(calls) {
		workerCount = len(calls)
	}
	jobs := make(chan job)
	results := make(chan indexedToolExecution, len(calls))
	errorsFound := make(chan indexedToolError, len(calls))
	var workers sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for current := range jobs {
				if ctx.Err() != nil {
					continue
				}
				outcome, err := gate.executor.Execute(ctx, current.call)
				if err != nil {
					errorsFound <- indexedToolError{index: current.index, err: err}
					continue
				}
				results <- indexedToolExecution{index: current.index, outcome: outcome}
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
	completed := make([]indexedToolExecution, 0, len(calls))
	for result := range results {
		completed = append(completed, result)
	}
	internalErrors := make([]indexedToolError, 0, len(errorsFound))
	for executionError := range errorsFound {
		internalErrors = append(internalErrors, executionError)
	}
	if len(internalErrors) > 0 {
		sort.Slice(internalErrors, func(left, right int) bool { return internalErrors[left].index < internalErrors[right].index })
		return nil, internalErrors[0].err
	}
	sort.Slice(completed, func(left, right int) bool { return completed[left].index < completed[right].index })
	return completed, nil
}

func indexToolSpecs(specs []tool.Spec) (map[string]tool.Spec, error) {
	indexed := make(map[string]tool.Spec, len(specs))
	for _, spec := range specs {
		if _, duplicate := indexed[spec.Name]; duplicate {
			return nil, fmt.Errorf("tool execution gate has duplicate tool spec %q", spec.Name)
		}
		indexed[spec.Name] = spec
	}
	return indexed, nil
}

func toolConcurrency(call tool.Call, specs map[string]tool.Spec) tool.ToolConcurrency {
	spec, ok := specs[call.Name]
	if !ok || spec.Concurrency != tool.ToolConcurrencyShared {
		return tool.ToolConcurrencyExclusive
	}
	return tool.ToolConcurrencyShared
}
