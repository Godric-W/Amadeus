package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
)

type schedulerExecutor struct {
	mu        sync.Mutex
	started   []TaskID
	active    int
	maxActive int
}

func (executor *schedulerExecutor) Execute(_ context.Context, input TaskExecutionInput) (DirectRunResult, error) {
	executor.mu.Lock()
	executor.started = append(executor.started, input.Task.ID)
	executor.active++
	if executor.active > executor.maxActive {
		executor.maxActive = executor.active
	}
	executor.mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	executor.mu.Lock()
	executor.active--
	executor.mu.Unlock()
	goal := Goal{Objective: input.Task.Objective}
	state := NewRun(RunID("sub/"+string(input.Task.ID)), goal, NewDirectGraph(goal), Budget{})
	state.Status = RunStatusCompleted
	state.Graph.Tasks[0].Status = TaskStatusCompleted
	state.Graph.Tasks[0].Result = &TaskResult{Summary: "done-" + string(input.Task.ID)}
	message := llm.AssistantMessage(state.Graph.Tasks[0].Result.Summary)
	return DirectRunResult{State: state, FinalMessage: &message}, nil
}

func TestSchedulerRunsDependenciesInOrderAndSerializesIndependentReads(t *testing.T) {
	scheduler, err := NewScheduler(SchedulerOptions{MaxParallelTasks: 2})
	if err != nil {
		t.Fatal(err)
	}
	graph := ExecutionGraph{Kind: ExecutionPlanned, Version: 1, Tasks: []Task{
		{ID: "a", Objective: "a", Status: TaskStatusPending, SideEffect: "read", Resources: []string{"a"}},
		{ID: "b", Objective: "b", Status: TaskStatusPending, SideEffect: "read", Resources: []string{"b"}},
		{ID: "c", Objective: "c", Status: TaskStatusPending, Dependencies: []TaskID{"a", "b"}, SideEffect: "write", Resources: []string{"c"}},
	}}
	executor := &schedulerExecutor{}
	result, err := scheduler.Run(context.Background(), TaskExecutionInput{RunID: "run-1", Budget: BudgetState{Budget: Budget{MaxSteps: 10}}}, graph, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !allTasksCompleted(result.Graph) || executor.maxActive != 1 {
		t.Fatalf("scheduler did not complete serially as expected: graph=%#v max=%d", result.Graph, executor.maxActive)
	}
	if len(executor.started) != 3 || executor.started[2] != "c" {
		t.Fatalf("dependency order not preserved: %v", executor.started)
	}
}

func TestSchedulerSerializesConflictingReadResources(t *testing.T) {
	scheduler, _ := NewScheduler(SchedulerOptions{MaxParallelTasks: 2})
	graph := ExecutionGraph{Kind: ExecutionPlanned, Version: 1, Tasks: []Task{
		{ID: "a", Objective: "a", Status: TaskStatusPending, SideEffect: "read", Resources: []string{"same"}},
		{ID: "b", Objective: "b", Status: TaskStatusPending, SideEffect: "read", Resources: []string{"same"}},
	}}
	executor := &schedulerExecutor{}
	result, err := scheduler.Run(context.Background(), TaskExecutionInput{RunID: "run-1"}, graph, executor)
	if err != nil || !allTasksCompleted(result.Graph) || executor.maxActive != 1 {
		t.Fatalf("conflicting resources were parallelized: result=%#v max=%d err=%v", result, executor.maxActive, err)
	}
}

func TestSchedulerSerializesIndependentWriteTasks(t *testing.T) {
	scheduler, _ := NewScheduler(SchedulerOptions{MaxParallelTasks: 2})
	graph := ExecutionGraph{Kind: ExecutionPlanned, Version: 1, Tasks: []Task{
		{ID: "a", Objective: "a", Status: TaskStatusPending, SideEffect: "write", Resources: []string{"a"}},
		{ID: "b", Objective: "b", Status: TaskStatusPending, SideEffect: "write", Resources: []string{"b"}},
	}}
	executor := &schedulerExecutor{}
	result, err := scheduler.Run(context.Background(), TaskExecutionInput{RunID: "run-1"}, graph, executor)
	if err != nil || !allTasksCompleted(result.Graph) || executor.maxActive != 1 {
		t.Fatalf("write tasks were not serialized: result=%#v max=%d err=%v", result, executor.maxActive, err)
	}
}

type schedulerTerminalExecutor struct {
	status     RunStatus
	stopReason StopReason
	reason     string
}

func (executor schedulerTerminalExecutor) Execute(_ context.Context, input TaskExecutionInput) (DirectRunResult, error) {
	goal := Goal{Objective: input.Task.Objective}
	state := NewRun("terminal", goal, NewDirectGraph(goal), input.Budget.Budget)
	state.Status = executor.status
	state.StopReason = executor.stopReason
	return DirectRunResult{State: state, Reason: executor.reason}, nil
}

func TestSchedulerPropagatesBlockedAndFailedTaskState(t *testing.T) {
	graph := ExecutionGraph{Kind: ExecutionPlanned, Version: 1, Tasks: []Task{{ID: "task", Objective: "task", Status: TaskStatusPending}}}
	tests := []struct {
		name           string
		executor       schedulerTerminalExecutor
		expectedStatus TaskStatus
	}{
		{name: "blocked", executor: schedulerTerminalExecutor{status: RunStatusSuspended, stopReason: StopReasonUserInputRequired, reason: "need input"}, expectedStatus: TaskStatusBlocked},
		{name: "failed", executor: schedulerTerminalExecutor{status: RunStatusFailed, stopReason: StopReasonToolError, reason: "tool failed"}, expectedStatus: TaskStatusFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scheduler, _ := NewScheduler(SchedulerOptions{MaxParallelTasks: 1})
			result, err := scheduler.Run(context.Background(), TaskExecutionInput{RunID: "run"}, graph, test.executor)
			if err != nil || result.Graph.Tasks[0].Status != test.expectedStatus || result.StopReason != test.executor.stopReason || result.Reason != test.executor.reason {
				t.Fatalf("terminal task state was not propagated: result=%#v err=%v", result, err)
			}
		})
	}
}
