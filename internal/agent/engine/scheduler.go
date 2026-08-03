package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type TaskExecutionInput struct {
	RunID          RunID
	Task           Task
	Messages       []llm.Message
	AvailableTools []tool.Spec
	PriorSteps     []Step
	Evidence       []Evidence
	Budget         BudgetState
}

type TaskExecutor interface {
	Execute(context.Context, TaskExecutionInput) (DirectRunResult, error)
}

type DirectTaskExecutor struct {
	engine *DirectEngine
}

func NewDirectTaskExecutor(engine *DirectEngine) (*DirectTaskExecutor, error) {
	if engine == nil {
		return nil, errors.New("direct task executor engine is nil")
	}
	return &DirectTaskExecutor{engine: engine}, nil
}

func (executor *DirectTaskExecutor) Execute(ctx context.Context, input TaskExecutionInput) (DirectRunResult, error) {
	if executor == nil || executor.engine == nil {
		return DirectRunResult{}, errors.New("direct task executor is nil")
	}
	goal := Goal{Objective: input.Task.Objective, AcceptanceCriteria: append([]Criterion(nil), input.Task.AcceptanceCriteria...)}
	budget := input.Task.Budget
	if budget == (Budget{}) {
		budget = input.Budget.Budget
	}
	state := NewRun(RunID(string(input.RunID)+"/"+string(input.Task.ID)), goal, NewDirectGraph(goal), budget)
	state.Graph.Tasks[0].SideEffect = input.Task.SideEffect
	state.Graph.Tasks[0].Resources = append([]string(nil), input.Task.Resources...)
	state.Evidence = cloneEvidence(input.Evidence)
	return executor.engine.Run(ctx, DirectRunInput{
		State: state, Messages: cloneLLMMessages(input.Messages), AvailableTools: cloneSchedulerTools(input.AvailableTools), PriorSteps: append([]Step(nil), input.PriorSteps...),
	})
}

func cloneSchedulerTools(specs []tool.Spec) []tool.Spec {
	cloned := make([]tool.Spec, len(specs))
	for index, spec := range specs {
		cloned[index] = spec.Clone()
	}
	return cloned
}

type SchedulerOptions struct {
	MaxParallelTasks int
}

type Scheduler struct {
	options SchedulerOptions
}

type ScheduleResult struct {
	Graph        ExecutionGraph
	Steps        []Step
	Evidence     []Evidence
	Budget       BudgetState
	FinalMessage *llm.Message
	ReplanTask   *Task
	Reason       string
	StopReason   StopReason
}

func NewScheduler(options SchedulerOptions) (*Scheduler, error) {
	if options.MaxParallelTasks < 0 {
		return nil, errors.New("scheduler maximum parallel tasks cannot be negative")
	}
	options.MaxParallelTasks = 1
	return &Scheduler{options: options}, nil
}

func (scheduler *Scheduler) Run(ctx context.Context, input TaskExecutionInput, graph ExecutionGraph, executor TaskExecutor) (ScheduleResult, error) {
	if scheduler == nil {
		return ScheduleResult{}, errors.New("scheduler is nil")
	}
	if executor == nil {
		return ScheduleResult{}, errors.New("scheduler task executor is nil")
	}
	if err := graph.Validate(); err != nil {
		return ScheduleResult{}, err
	}
	result := ScheduleResult{Graph: cloneGraph(graph), Budget: input.Budget}
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if allTasksCompleted(result.Graph) {
			return result, nil
		}
		ready := readyTasks(result.Graph)
		if len(ready) == 0 {
			return result, errors.New("scheduler found no ready task while graph is incomplete")
		}
		batch := selectTaskBatch(ready, scheduler.options.MaxParallelTasks)
		for _, task := range batch {
			for index := range result.Graph.Tasks {
				if result.Graph.Tasks[index].ID == task.ID {
					result.Graph.Tasks[index].Status = TaskStatusRunning
				}
			}
		}
		type execution struct {
			task   Task
			result DirectRunResult
			err    error
		}
		results := make(chan execution, len(batch))
		var waitGroup sync.WaitGroup
		for _, task := range batch {
			task := task
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				priorSteps := append([]Step(nil), input.PriorSteps...)
				priorSteps = append(priorSteps, result.Steps...)
				evidence := mergeEvidence(input.Evidence, result.Evidence)
				runResult, err := executor.Execute(ctx, TaskExecutionInput{
					RunID: input.RunID, Task: task, Messages: input.Messages, AvailableTools: input.AvailableTools,
					PriorSteps: priorSteps, Evidence: evidence, Budget: result.Budget,
				})
				results <- execution{task: task, result: runResult, err: err}
			}()
		}
		waitGroup.Wait()
		close(results)
		collected := make([]execution, 0, len(batch))
		for item := range results {
			collected = append(collected, item)
		}
		sort.Slice(collected, func(left, right int) bool { return collected[left].task.ID < collected[right].task.ID })
		for _, item := range collected {
			if item.err != nil {
				return result, item.err
			}
			result.Steps = append(result.Steps, item.result.Steps...)
			result.Evidence = mergeEvidence(result.Evidence, item.result.State.Evidence)
			result.Budget = addBudgetStates(result.Budget, item.result.State.Budget)
			if item.result.FinalMessage != nil {
				message := *item.result.FinalMessage
				result.FinalMessage = &message
			}
			position := graphTaskPosition(result.Graph, item.task.ID)
			if position < 0 {
				return result, fmt.Errorf("scheduler task %q disappeared from execution graph", item.task.ID)
			}
			task := &result.Graph.Tasks[position]
			switch item.result.State.Status {
			case RunStatusCompleted:
				task.Status = TaskStatusCompleted
				if len(item.result.State.Graph.Tasks) > 0 {
					task.Result = cloneTaskResult(item.result.State.Graph.Tasks[0].Result)
				}
			case RunStatusPlanning:
				task.Status = TaskStatusBlocked
				copyTask := *task
				result.ReplanTask = &copyTask
				result.Reason, result.StopReason = item.result.Reason, StopReasonReplanExhausted
				return result, nil
			case RunStatusSuspended:
				task.Status = TaskStatusBlocked
				result.Reason, result.StopReason = item.result.Reason, StopReasonUserInputRequired
				return result, nil
			case RunStatusCancelled:
				task.Status = TaskStatusCancelled
				result.Reason, result.StopReason = item.result.Reason, StopReasonCancelled
				return result, nil
			default:
				task.Status = TaskStatusFailed
				result.Reason, result.StopReason = item.result.Reason, item.result.State.StopReason
				return result, nil
			}
		}
	}
}

func readyTasks(graph ExecutionGraph) []Task {
	ready := make([]Task, 0)
	for _, task := range graph.Tasks {
		if task.Status != TaskStatusPending && task.Status != TaskStatusReady {
			continue
		}
		allCompleted := true
		for _, dependency := range task.Dependencies {
			if dependent := graphTask(graph, dependency); dependent.Status != TaskStatusCompleted {
				allCompleted = false
				break
			}
		}
		if allCompleted {
			ready = append(ready, task)
		}
	}
	sort.Slice(ready, func(left, right int) bool { return ready[left].ID < ready[right].ID })
	return ready
}

func selectTaskBatch(ready []Task, maximum int) []Task {
	if len(ready) <= maximum {
		maximum = len(ready)
	}
	batch := make([]Task, 0, maximum)
	for _, task := range ready {
		if len(batch) == maximum {
			break
		}
		if task.SideEffect != tool.SideEffectRead {
			if len(batch) == 0 {
				return []Task{task}
			}
			break
		}
		conflict := false
		for _, selected := range batch {
			if resourcesOverlap(task.Resources, selected.Resources) {
				conflict = true
				break
			}
		}
		if !conflict {
			batch = append(batch, task)
		}
	}
	if len(batch) == 0 {
		return []Task{ready[0]}
	}
	return batch
}

func resourcesOverlap(left, right []string) bool {
	seen := make(map[string]struct{}, len(left))
	for _, resource := range left {
		seen[strings.TrimSpace(resource)] = struct{}{}
	}
	for _, resource := range right {
		if _, exists := seen[strings.TrimSpace(resource)]; exists {
			return true
		}
	}
	return false
}

func allTasksCompleted(graph ExecutionGraph) bool {
	for _, task := range graph.Tasks {
		if task.Status != TaskStatusCompleted {
			return false
		}
	}
	return true
}

func graphTask(graph ExecutionGraph, id TaskID) Task {
	for _, task := range graph.Tasks {
		if task.ID == id {
			return task
		}
	}
	return Task{ID: id, Status: TaskStatusFailed}
}

func graphTaskPosition(graph ExecutionGraph, id TaskID) int {
	for index, task := range graph.Tasks {
		if task.ID == id {
			return index
		}
	}
	return -1
}

func cloneGraph(graph ExecutionGraph) ExecutionGraph {
	cloned := graph
	cloned.Tasks = make([]Task, len(graph.Tasks))
	for index, task := range graph.Tasks {
		cloned.Tasks[index] = task
		cloned.Tasks[index].Dependencies = append([]TaskID(nil), task.Dependencies...)
		cloned.Tasks[index].AcceptanceCriteria = append([]Criterion(nil), task.AcceptanceCriteria...)
		cloned.Tasks[index].Resources = append([]string(nil), task.Resources...)
		cloned.Tasks[index].Result = cloneTaskResult(task.Result)
	}
	return cloned
}

func cloneTaskResult(result *TaskResult) *TaskResult {
	if result == nil {
		return nil
	}
	cloned := *result
	cloned.EvidenceIDs = append([]EvidenceID(nil), result.EvidenceIDs...)
	return &cloned
}
