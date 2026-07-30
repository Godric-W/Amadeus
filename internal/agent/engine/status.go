package engine

import "fmt"

type RunStatus string

const (
	RunStatusInitialized       RunStatus = "initialized"
	RunStatusStrategySelecting RunStatus = "strategy_selecting"
	RunStatusPlanning          RunStatus = "planning"
	RunStatusPlanReady         RunStatus = "plan_ready"
	RunStatusScheduling        RunStatus = "scheduling"
	RunStatusTaskRunning       RunStatus = "task_running"
	RunStatusVerifyingTask     RunStatus = "verifying_task"
	RunStatusReflectingTask    RunStatus = "reflecting_task"
	RunStatusVerifyingRun      RunStatus = "verifying_run"
	RunStatusReflectingFinal   RunStatus = "reflecting_final"
	RunStatusSynthesizing      RunStatus = "synthesizing"
	RunStatusSuspended         RunStatus = "suspended"
	RunStatusCompleted         RunStatus = "completed"
	RunStatusFailed            RunStatus = "failed"
	RunStatusCancelled         RunStatus = "cancelled"
)

func (status RunStatus) Valid() bool {
	_, exists := runTransitions[status]
	return exists
}

type TaskStatus string

const (
	TaskStatusPending           TaskStatus = "pending"
	TaskStatusReady             TaskStatus = "ready"
	TaskStatusRunning           TaskStatus = "running"
	TaskStatusCandidateComplete TaskStatus = "candidate_complete"
	TaskStatusVerifying         TaskStatus = "verifying"
	TaskStatusReflecting        TaskStatus = "reflecting"
	TaskStatusCompleted         TaskStatus = "completed"
	TaskStatusFailed            TaskStatus = "failed"
	TaskStatusBlocked           TaskStatus = "blocked"
	TaskStatusCancelled         TaskStatus = "cancelled"
)

func (status TaskStatus) Valid() bool {
	_, exists := taskTransitions[status]
	return exists
}

type StopReason string

const (
	StopReasonCompleted          StopReason = "completed"
	StopReasonCancelled          StopReason = "cancelled"
	StopReasonMaxSteps           StopReason = "max_steps"
	StopReasonBudgetExceeded     StopReason = "budget_exceeded"
	StopReasonProviderError      StopReason = "provider_error"
	StopReasonToolError          StopReason = "tool_error"
	StopReasonVerificationFailed StopReason = "verification_failed"
	StopReasonReplanExhausted    StopReason = "replan_exhausted"
	StopReasonUserInputRequired  StopReason = "user_input_required"
)

func (reason StopReason) Valid() bool {
	switch reason {
	case StopReasonCompleted, StopReasonCancelled, StopReasonMaxSteps, StopReasonBudgetExceeded,
		StopReasonProviderError, StopReasonToolError, StopReasonVerificationFailed,
		StopReasonReplanExhausted, StopReasonUserInputRequired:
		return true
	default:
		return false
	}
}

type TransitionError struct {
	Entity string
	From   string
	To     string
}

func (err *TransitionError) Error() string {
	return fmt.Sprintf("invalid %s status transition from %q to %q", err.Entity, err.From, err.To)
}

func (state *RunState) Transition(next RunStatus) error {
	if !canTransitionRun(state.Status, next) {
		return &TransitionError{Entity: "run", From: string(state.Status), To: string(next)}
	}
	state.Status = next
	return nil
}

func (task *Task) Transition(next TaskStatus) error {
	if !canTransitionTask(task.Status, next) {
		return &TransitionError{Entity: "task", From: string(task.Status), To: string(next)}
	}
	task.Status = next
	return nil
}

func canTransitionRun(from, to RunStatus) bool {
	for _, allowed := range runTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

func canTransitionTask(from, to TaskStatus) bool {
	for _, allowed := range taskTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

var runTransitions = map[RunStatus][]RunStatus{
	RunStatusInitialized:       {RunStatusStrategySelecting, RunStatusCancelled, RunStatusFailed},
	RunStatusStrategySelecting: {RunStatusPlanning, RunStatusTaskRunning, RunStatusCancelled, RunStatusFailed},
	RunStatusPlanning:          {RunStatusPlanReady, RunStatusSuspended, RunStatusCancelled, RunStatusFailed},
	RunStatusPlanReady:         {RunStatusScheduling, RunStatusSuspended, RunStatusCancelled, RunStatusFailed},
	RunStatusScheduling:        {RunStatusTaskRunning, RunStatusVerifyingRun, RunStatusSuspended, RunStatusCancelled, RunStatusFailed},
	RunStatusTaskRunning:       {RunStatusVerifyingTask, RunStatusPlanning, RunStatusSuspended, RunStatusCancelled, RunStatusFailed},
	RunStatusVerifyingTask:     {RunStatusReflectingTask, RunStatusCancelled, RunStatusFailed},
	RunStatusReflectingTask:    {RunStatusScheduling, RunStatusTaskRunning, RunStatusPlanning, RunStatusSuspended, RunStatusCancelled, RunStatusFailed},
	RunStatusVerifyingRun:      {RunStatusReflectingFinal, RunStatusCancelled, RunStatusFailed},
	RunStatusReflectingFinal:   {RunStatusSynthesizing, RunStatusTaskRunning, RunStatusPlanning, RunStatusSuspended, RunStatusCancelled, RunStatusFailed},
	RunStatusSynthesizing:      {RunStatusCompleted, RunStatusCancelled, RunStatusFailed},
	RunStatusSuspended:         {RunStatusStrategySelecting, RunStatusPlanning, RunStatusScheduling, RunStatusTaskRunning, RunStatusCancelled, RunStatusFailed},
	RunStatusCompleted:         {},
	RunStatusFailed:            {},
	RunStatusCancelled:         {},
}

var taskTransitions = map[TaskStatus][]TaskStatus{
	TaskStatusPending:           {TaskStatusReady, TaskStatusBlocked, TaskStatusCancelled},
	TaskStatusReady:             {TaskStatusRunning, TaskStatusBlocked, TaskStatusCancelled},
	TaskStatusRunning:           {TaskStatusCandidateComplete, TaskStatusBlocked, TaskStatusFailed, TaskStatusCancelled},
	TaskStatusCandidateComplete: {TaskStatusVerifying, TaskStatusFailed, TaskStatusCancelled},
	TaskStatusVerifying:         {TaskStatusReflecting, TaskStatusFailed, TaskStatusCancelled},
	TaskStatusReflecting:        {TaskStatusCompleted, TaskStatusReady, TaskStatusBlocked, TaskStatusFailed, TaskStatusCancelled},
	TaskStatusCompleted:         {},
	TaskStatusFailed:            {},
	TaskStatusBlocked:           {TaskStatusReady, TaskStatusFailed, TaskStatusCancelled},
	TaskStatusCancelled:         {},
}
