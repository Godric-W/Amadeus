package react

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type indexedToolExecution struct {
	index     int
	execution ToolExecution
}

type resourceExecutor struct {
	executor    CallExecutor
	maxParallel int
}

func newResourceExecutor(executor CallExecutor, maxParallel int) (*resourceExecutor, error) {
	if executor == nil {
		return nil, errors.New("resource executor call executor is nil")
	}
	if maxParallel <= 0 {
		return nil, errors.New("resource executor max parallelism must be greater than zero")
	}
	return &resourceExecutor{executor: executor, maxParallel: maxParallel}, nil
}

func (executor *resourceExecutor) Execute(ctx context.Context, calls []tool.Call, specs []tool.Spec) ([]indexedToolExecution, error) {
	indexedSpecs, err := indexExecutionSpecs(specs)
	if err != nil {
		return nil, err
	}
	completed := make([]indexedToolExecution, 0, len(calls))
	for index := 0; index < len(calls); {
		if ctx.Err() != nil {
			break
		}
		if !parallelEligible(calls[index], indexedSpecs) {
			execution, _ := executor.executor.Execute(ctx, calls[index])
			completed = append(completed, indexedToolExecution{index: index, execution: execution})
			index++
			continue
		}
		end := index + 1
		for end < len(calls) && parallelEligible(calls[end], indexedSpecs) {
			end++
		}
		group, err := executor.executeParallel(ctx, calls[index:end], indexedSpecs, index)
		if err != nil {
			return nil, err
		}
		completed = append(completed, group...)
		index = end
	}
	sort.Slice(completed, func(left, right int) bool { return completed[left].index < completed[right].index })
	return completed, nil
}

func (executor *resourceExecutor) executeParallel(ctx context.Context, calls []tool.Call, specs map[string]tool.Spec, offset int) ([]indexedToolExecution, error) {
	type job struct {
		index int
		call  tool.Call
		keys  []string
	}
	jobs := make([]job, len(calls))
	locks := make(map[string]*sync.Mutex)
	for index, call := range calls {
		keys, err := callResourceKeys(call, specs[call.Name])
		if err != nil {
			return nil, err
		}
		jobs[index] = job{index: offset + index, call: call, keys: keys}
		for _, key := range keys {
			if _, exists := locks[key]; !exists {
				locks[key] = &sync.Mutex{}
			}
		}
	}
	workerCount := executor.maxParallel
	if workerCount > len(jobs) {
		workerCount = len(jobs)
	}
	jobChannel := make(chan job)
	resultChannel := make(chan indexedToolExecution, len(jobs))
	var waitGroup sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for current := range jobChannel {
				if ctx.Err() != nil {
					continue
				}
				for _, key := range current.keys {
					locks[key].Lock()
				}
				if ctx.Err() == nil {
					execution, _ := executor.executor.Execute(ctx, current.call)
					resultChannel <- indexedToolExecution{index: current.index, execution: execution}
				}
				for index := len(current.keys) - 1; index >= 0; index-- {
					locks[current.keys[index]].Unlock()
				}
			}
		}()
	}
	go func() {
		defer close(jobChannel)
		for _, current := range jobs {
			select {
			case jobChannel <- current:
			case <-ctx.Done():
				return
			}
		}
	}()
	waitGroup.Wait()
	close(resultChannel)
	results := make([]indexedToolExecution, 0, len(jobs))
	for result := range resultChannel {
		results = append(results, result)
	}
	sort.Slice(results, func(left, right int) bool { return results[left].index < results[right].index })
	return results, nil
}

func indexExecutionSpecs(specs []tool.Spec) (map[string]tool.Spec, error) {
	indexed := make(map[string]tool.Spec, len(specs))
	for _, spec := range specs {
		if _, duplicate := indexed[spec.Name]; duplicate {
			return nil, fmt.Errorf("resource executor has duplicate tool spec %q", spec.Name)
		}
		indexed[spec.Name] = spec
	}
	return indexed, nil
}

func parallelEligible(call tool.Call, specs map[string]tool.Spec) bool {
	spec, ok := specs[call.Name]
	if !ok || !spec.ParallelSafe || spec.ResourceStrategy.Mode == tool.ResourceModeExclusive {
		return false
	}
	if spec.SideEffect != tool.SideEffectNone && spec.SideEffect != tool.SideEffectRead {
		return false
	}
	if spec.ResourceStrategy.Mode == tool.ResourceModeArguments {
		_, err := callResourceKeys(call, spec)
		return err == nil
	}
	return true
}

func callResourceKeys(call tool.Call, spec tool.Spec) ([]string, error) {
	switch spec.ResourceStrategy.Mode {
	case tool.ResourceModeNone:
		return nil, nil
	case tool.ResourceModeExclusive:
		return []string{"*"}, nil
	case tool.ResourceModeArguments:
		decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
		decoder.UseNumber()
		var arguments any
		if err := decoder.Decode(&arguments); err != nil {
			return nil, fmt.Errorf("decode resource arguments for %q: %w", call.Name, err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("decode trailing resource arguments for %q", call.Name)
		}
		keys := make([]string, 0, len(spec.ResourceStrategy.ArgumentPaths))
		for _, argumentPath := range spec.ResourceStrategy.ArgumentPaths {
			value := jsonPointerValue(arguments, argumentPath)
			canonical, err := json.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("canonicalize resource argument %q: %w", argumentPath, err)
			}
			keys = append(keys, normalizeArgumentPath(argumentPath)+"="+string(canonical))
		}
		sort.Strings(keys)
		return keys, nil
	default:
		return nil, fmt.Errorf("unsupported resource strategy %q", spec.ResourceStrategy.Mode)
	}
}

func jsonPointerValue(document any, pointer string) any {
	pointer = normalizeArgumentPath(pointer)
	if pointer == "" {
		return document
	}
	current := document
	for _, token := range strings.Split(pointer, "/") {
		if token == "" {
			continue
		}
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = object[token]
		if !ok {
			return nil
		}
	}
	return current
}

func normalizeArgumentPath(pointer string) string {
	pointer = strings.TrimSpace(pointer)
	if pointer == "" {
		return ""
	}
	if !strings.HasPrefix(pointer, "/") {
		pointer = "/" + pointer
	}
	return pointer
}
