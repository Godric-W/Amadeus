package plan

import "fmt"

type RunStatus string

const (
	RunStatusInitialized RunStatus = "initialized"
	RunStatusPlanning    RunStatus = "planning"
	RunStatusScheduling  RunStatus = "scheduling"
	RunStatusTaskRunning RunStatus = "task_running"
	RunStatusCompleted   RunStatus = "completed"
	RunStatusFailed      RunStatus = "failed"
	RunStatusCancelled   RunStatus = "cancelled"
)

func (status RunStatus) Valid() bool {
	_, exists := runTransitions[status]
	return exists
}

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusBlocked   TaskStatus = "blocked"
	TaskStatusCancelled TaskStatus = "cancelled"
)

func (status TaskStatus) Valid() bool {
	_, exists := taskTransitions[status]
	return exists
}

type StopReason string

const (
	StopReasonCompleted         StopReason = "completed"
	StopReasonCancelled         StopReason = "cancelled"
	StopReasonMaxIterations     StopReason = "max_iterations"
	StopReasonBudgetExceeded    StopReason = "budget_exceeded"
	StopReasonProviderError     StopReason = "provider_error"
	StopReasonToolError         StopReason = "tool_error"
	StopReasonReplanExhausted   StopReason = "replan_exhausted"
	StopReasonUserInputRequired StopReason = "user_input_required"
)

func (reason StopReason) Valid() bool {
	switch reason {
	case StopReasonCompleted, StopReasonCancelled, StopReasonMaxIterations, StopReasonBudgetExceeded,
		StopReasonProviderError, StopReasonToolError, StopReasonReplanExhausted, StopReasonUserInputRequired:
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
	RunStatusInitialized: {RunStatusPlanning, RunStatusCancelled, RunStatusFailed},
	RunStatusPlanning:    {RunStatusScheduling, RunStatusCompleted, RunStatusCancelled, RunStatusFailed},
	RunStatusScheduling:  {RunStatusTaskRunning, RunStatusPlanning, RunStatusCompleted, RunStatusCancelled, RunStatusFailed},
	RunStatusTaskRunning: {RunStatusScheduling, RunStatusPlanning, RunStatusCompleted, RunStatusCancelled, RunStatusFailed},
	RunStatusCompleted:   {},
	RunStatusFailed:      {},
	RunStatusCancelled:   {},
}

var taskTransitions = map[TaskStatus][]TaskStatus{
	TaskStatusPending:   {TaskStatusRunning, TaskStatusBlocked, TaskStatusCancelled},
	TaskStatusRunning:   {TaskStatusCompleted, TaskStatusBlocked, TaskStatusFailed, TaskStatusCancelled},
	TaskStatusCompleted: {},
	TaskStatusFailed:    {},
	TaskStatusBlocked:   {TaskStatusFailed, TaskStatusCancelled},
	TaskStatusCancelled: {},
}
