package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/event"
)

type ReActEngine struct {
	runner TaskRunner
	events event.Sink
}

func NewReActEngine(runner TaskRunner, events event.Sink) (*ReActEngine, error) {
	if runner == nil {
		return nil, errors.New("ReAct engine task runner is nil")
	}
	return &ReActEngine{runner: runner, events: events}, nil
}

func (engine *ReActEngine) Run(ctx context.Context, input DirectRunInput) (DirectRunResult, error) {
	if engine == nil || engine.runner == nil {
		return DirectRunResult{}, errors.New("ReAct engine is nil")
	}
	if ctx == nil {
		return DirectRunResult{}, errors.New("ReAct engine context is nil")
	}
	state := input.State
	if err := validateReActStart(state); err != nil {
		return DirectRunResult{}, err
	}
	result := DirectRunResult{State: state, Steps: append([]Step(nil), input.PriorSteps...)}
	if err := engine.publish(ctx, event.EngineRunStarted{RunID: string(state.ID), TaskID: string(state.Graph.Tasks[0].ID)}); err != nil {
		return result, err
	}
	result.State.Status = RunStatusTaskRunning
	task := &result.State.Graph.Tasks[0]
	task.Status = TaskStatusRunning
	task.Attempts = 1
	result.State.ActiveTaskID = task.ID
	if err := engine.publish(ctx, event.EngineStatusChanged{
		RunID: string(state.ID), Entity: "run", EntityID: string(state.ID), From: string(RunStatusInitialized), To: string(RunStatusTaskRunning),
	}); err != nil {
		return result, err
	}
	if err := engine.publish(ctx, event.EngineStatusChanged{
		RunID: string(state.ID), Entity: "task", EntityID: string(task.ID), From: string(TaskStatusPending), To: string(TaskStatusRunning),
	}); err != nil {
		return result, err
	}

	outcome, err := engine.runner.Run(ctx, TaskRunInput{
		RunID: result.State.ID, Task: *task, Messages: cloneLLMMessages(input.Messages), AvailableTools: input.AvailableTools,
		PriorSteps: result.Steps, Evidence: cloneEvidence(result.State.Evidence), Budget: result.State.Budget,
	})
	if err != nil {
		return engine.fail(ctx, result, task, StopReasonProviderError, err.Error(), err)
	}
	if err := outcome.Validate(); err != nil {
		return engine.fail(ctx, result, task, StopReasonProviderError, err.Error(), fmt.Errorf("validate ReAct task outcome: %w", err))
	}
	result.Steps = append(result.Steps, outcome.Steps...)
	result.State.Evidence = mergeEvidence(result.State.Evidence, outcome.Evidence)
	result.State.Budget = mergeBudget(result.State.Budget, outcome.Budget)

	switch outcome.Kind {
	case TaskOutcomeCandidateComplete:
		task.Status = TaskStatusCompleted
		task.Result = &outcome.Candidate.Result
		result.State.Status = RunStatusCompleted
		result.State.StopReason = StopReasonCompleted
		result.State.ActiveTaskID = ""
		message := outcome.Candidate.FinalMessage
		result.FinalMessage = &message
		if err := engine.publishTerminal(ctx, result, TaskStatusRunning, TaskStatusCompleted, ""); err != nil {
			return result, err
		}
		return result, nil
	case TaskOutcomeNeedsPlan:
		reason := strings.TrimSpace(outcome.Reason)
		if reason == "" {
			reason = "the task requires explicit planning"
		}
		reason += "; retry with /plan <task>"
		task.Status = TaskStatusBlocked
		result.State.Status = RunStatusSuspended
		result.State.StopReason = StopReasonUserInputRequired
		result.Reason = reason
		if err := engine.publishTerminal(ctx, result, TaskStatusRunning, TaskStatusBlocked, reason); err != nil {
			return result, err
		}
		return result, nil
	case TaskOutcomeBlocked:
		task.Status = TaskStatusBlocked
		result.State.Status = RunStatusSuspended
		result.State.StopReason = StopReasonUserInputRequired
		result.Reason = outcome.Reason
		if err := engine.publishTerminal(ctx, result, TaskStatusRunning, TaskStatusBlocked, outcome.Reason); err != nil {
			return result, err
		}
		return result, nil
	case TaskOutcomeFailed:
		return engine.fail(ctx, result, task, outcome.StopReason, outcome.Reason, nil)
	case TaskOutcomeCancelled:
		task.Status = TaskStatusCancelled
		result.State.Status = RunStatusCancelled
		result.State.StopReason = StopReasonCancelled
		result.Reason = outcome.Reason
		if err := engine.publishTerminal(ctx, result, TaskStatusRunning, TaskStatusCancelled, outcome.Reason); err != nil {
			return result, err
		}
		return result, nil
	default:
		return engine.fail(ctx, result, task, StopReasonProviderError, fmt.Sprintf("unsupported ReAct outcome %q", outcome.Kind), nil)
	}
}

func validateReActStart(state RunState) error {
	if strings.TrimSpace(string(state.ID)) == "" {
		return errors.New("ReAct engine run ID is empty")
	}
	if state.Status != RunStatusInitialized {
		return fmt.Errorf("ReAct engine run status must be %q", RunStatusInitialized)
	}
	if state.Graph.Kind != ExecutionDirect || len(state.Graph.Tasks) != 1 {
		return errors.New("ReAct engine requires a direct graph with exactly one root task")
	}
	if strings.TrimSpace(state.Goal.Objective) == "" || strings.TrimSpace(state.Graph.Tasks[0].Objective) == "" {
		return errors.New("ReAct engine objective is empty")
	}
	if state.Graph.Tasks[0].Status != TaskStatusPending {
		return fmt.Errorf("ReAct engine root task status must be %q", TaskStatusPending)
	}
	return state.Budget.Validate()
}

func (engine *ReActEngine) fail(ctx context.Context, result DirectRunResult, task *Task, reason StopReason, detail string, cause error) (DirectRunResult, error) {
	if !reason.Valid() || reason == StopReasonCompleted || reason == StopReasonCancelled || reason == StopReasonUserInputRequired {
		reason = StopReasonProviderError
	}
	task.Status = TaskStatusFailed
	result.State.Status = RunStatusFailed
	result.State.StopReason = reason
	result.Reason = strings.TrimSpace(detail)
	if err := engine.publishTerminal(ctx, result, TaskStatusRunning, TaskStatusFailed, result.Reason); err != nil {
		if cause != nil {
			return result, errors.Join(cause, err)
		}
		return result, err
	}
	return result, cause
}

func (engine *ReActEngine) publishTerminal(ctx context.Context, result DirectRunResult, from, to TaskStatus, reason string) error {
	publishCtx := context.WithoutCancel(ctx)
	if err := engine.publish(publishCtx, event.EngineStatusChanged{
		RunID: string(result.State.ID), Entity: "task", EntityID: string(result.State.Graph.Tasks[0].ID), From: string(from), To: string(to),
	}); err != nil {
		return err
	}
	return engine.publish(publishCtx, event.EngineRunCompleted{
		RunID: string(result.State.ID), Status: string(result.State.Status), StopReason: string(result.State.StopReason), Reason: reason,
	})
}

func (engine *ReActEngine) publish(ctx context.Context, item event.Event) error {
	if engine.events == nil {
		return nil
	}
	if err := engine.events.Publish(ctx, item); err != nil {
		return fmt.Errorf("publish ReAct engine event: %w", err)
	}
	return nil
}

var _ RunEngine = (*ReActEngine)(nil)
