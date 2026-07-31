package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/prompts"
)

type DirectEngineOptions struct {
	MaxAttempts int
	RetryPrompt string
}

type DirectRunInput struct {
	State          RunState      `json:"state"`
	Messages       []llm.Message `json:"messages,omitempty"`
	AvailableTools []tool.Spec   `json:"available_tools,omitempty"`
	PriorSteps     []Step        `json:"prior_steps,omitempty"`
}

type DirectRunResult struct {
	State        RunState      `json:"state"`
	Steps        []Step        `json:"steps,omitempty"`
	FinalMessage *llm.Message  `json:"final_message,omitempty"`
	Verification *Verification `json:"verification,omitempty"`
	Reflection   *Reflection   `json:"reflection,omitempty"`
	Reason       string        `json:"reason,omitempty"`
}

type DirectEngine struct {
	runner    TaskRunner
	verifier  Verifier
	reflector Reflector
	events    event.Sink
	options   DirectEngineOptions
}

func NewDirectEngine(runner TaskRunner, verifier Verifier, reflector Reflector, events event.Sink, options DirectEngineOptions) (*DirectEngine, error) {
	if runner == nil {
		return nil, errors.New("direct engine task runner is nil")
	}
	if verifier == nil {
		return nil, errors.New("direct engine verifier is nil")
	}
	if reflector == nil {
		return nil, errors.New("direct engine reflector is nil")
	}
	if events == nil {
		return nil, errors.New("direct engine event sink is nil")
	}
	if options.MaxAttempts <= 0 {
		return nil, errors.New("direct engine max attempts must be greater than zero")
	}
	options.RetryPrompt = strings.TrimSpace(options.RetryPrompt)
	if options.RetryPrompt == "" {
		options.RetryPrompt = prompts.RetryProtocol()
	}
	return &DirectEngine{runner: runner, verifier: verifier, reflector: reflector, events: events, options: options}, nil
}

func (engine *DirectEngine) Run(ctx context.Context, input DirectRunInput) (result DirectRunResult, runErr error) {
	state, err := cloneRunState(input.State)
	if err != nil {
		return DirectRunResult{}, err
	}
	if err := validateDirectStart(state); err != nil {
		return DirectRunResult{}, err
	}
	steps := append([]Step(nil), input.PriorSteps...)
	messages := cloneLLMMessages(input.Messages)
	task := &state.Graph.Tasks[0]
	state.ActiveTaskID = task.ID
	if err := engine.events.Publish(context.WithoutCancel(ctx), event.EngineRunStarted{RunID: string(state.ID), TaskID: string(task.ID)}); err != nil {
		return DirectRunResult{}, fmt.Errorf("publish engine run started: %w", err)
	}
	defer func() {
		if state.Status != RunStatusCompleted && state.Status != RunStatusFailed && state.Status != RunStatusCancelled {
			return
		}
		publishErr := engine.events.Publish(context.WithoutCancel(ctx), event.EngineRunCompleted{
			RunID: string(state.ID), Status: string(state.Status), StopReason: string(state.StopReason), Reason: result.Reason,
		})
		if publishErr != nil && runErr == nil {
			runErr = fmt.Errorf("publish engine run completed: %w", publishErr)
		}
	}()

	if err := ctx.Err(); err != nil {
		if transitionErr := engine.cancelDirect(ctx, &state, task); transitionErr != nil {
			return DirectRunResult{}, transitionErr
		}
		return DirectRunResult{State: state, Steps: steps, Reason: err.Error()}, nil
	}
	if err := engine.transitionRun(ctx, &state, RunStatusStrategySelecting); err != nil {
		return DirectRunResult{}, err
	}
	if err := engine.transitionTask(ctx, state.ID, task, TaskStatusReady); err != nil {
		return DirectRunResult{}, err
	}
	if err := engine.transitionRun(ctx, &state, RunStatusTaskRunning); err != nil {
		return DirectRunResult{}, err
	}

	var latestVerification *Verification
	var latestReflection *Reflection
	for {
		if task.Attempts >= engine.options.MaxAttempts {
			if err := engine.failDirect(ctx, &state, task, StopReasonVerificationFailed); err != nil {
				return DirectRunResult{}, err
			}
			return DirectRunResult{State: state, Steps: steps, Verification: latestVerification, Reflection: latestReflection, Reason: "task retry attempts exhausted"}, nil
		}
		if err := engine.transitionTask(ctx, state.ID, task, TaskStatusRunning); err != nil {
			return DirectRunResult{}, err
		}
		task.Attempts++

		outcome, err := engine.runner.Run(ctx, TaskRunInput{
			RunID: state.ID, Task: *task, Messages: messages, AvailableTools: input.AvailableTools,
			PriorSteps: steps, Evidence: state.Evidence, Budget: state.Budget,
		})
		if err != nil {
			_ = engine.failDirect(ctx, &state, task, StopReasonProviderError)
			return DirectRunResult{State: state, Steps: steps, Reason: err.Error()}, err
		}
		if err := outcome.Validate(); err != nil {
			_ = engine.failDirect(ctx, &state, task, StopReasonProviderError)
			return DirectRunResult{State: state, Steps: steps, Reason: err.Error()}, fmt.Errorf("validate task runner outcome: %w", err)
		}
		steps = append(steps, outcome.Steps...)
		state.Evidence = mergeEvidence(state.Evidence, outcome.Evidence)
		state.Budget = mergeBudget(state.Budget, outcome.Budget)

		switch outcome.Kind {
		case TaskOutcomeNeedsPlan:
			if err := engine.transitionTask(ctx, state.ID, task, TaskStatusBlocked); err != nil {
				return DirectRunResult{}, err
			}
			if err := engine.transitionRun(ctx, &state, RunStatusPlanning); err != nil {
				return DirectRunResult{}, err
			}
			return DirectRunResult{State: state, Steps: steps, Reason: outcome.Reason}, nil
		case TaskOutcomeBlocked:
			if err := engine.transitionTask(ctx, state.ID, task, TaskStatusBlocked); err != nil {
				return DirectRunResult{}, err
			}
			if err := engine.transitionRun(ctx, &state, RunStatusSuspended); err != nil {
				return DirectRunResult{}, err
			}
			state.StopReason = StopReasonUserInputRequired
			return DirectRunResult{State: state, Steps: steps, Reason: outcome.Reason}, nil
		case TaskOutcomeFailed:
			if err := engine.failDirect(ctx, &state, task, outcome.StopReason); err != nil {
				return DirectRunResult{}, err
			}
			return DirectRunResult{State: state, Steps: steps, Reason: outcome.Reason}, nil
		case TaskOutcomeCancelled:
			if err := engine.cancelDirect(ctx, &state, task); err != nil {
				return DirectRunResult{}, err
			}
			return DirectRunResult{State: state, Steps: steps, Reason: outcome.Reason}, nil
		case TaskOutcomeCandidateComplete:
			if outcome.Candidate == nil {
				return DirectRunResult{}, errors.New("direct engine candidate outcome has no candidate")
			}
			if err := engine.transitionTask(ctx, state.ID, task, TaskStatusCandidateComplete); err != nil {
				return DirectRunResult{}, err
			}
			if err := engine.transitionRun(ctx, &state, RunStatusVerifyingTask); err != nil {
				return DirectRunResult{}, err
			}
			if err := engine.transitionTask(ctx, state.ID, task, TaskStatusVerifying); err != nil {
				return DirectRunResult{}, err
			}

			verification, verifyErr := engine.verifier.Verify(ctx, VerificationInput{
				RunID: state.ID, Task: *task, Candidate: outcome.Candidate.Result, Evidence: state.Evidence,
			})
			if verifyErr != nil {
				_ = engine.failDirect(ctx, &state, task, StopReasonVerificationFailed)
				return DirectRunResult{State: state, Steps: steps, Reason: verifyErr.Error()}, verifyErr
			}
			if err := verification.Validate(); err != nil {
				_ = engine.failDirect(ctx, &state, task, StopReasonVerificationFailed)
				return DirectRunResult{State: state, Steps: steps, Reason: err.Error()}, fmt.Errorf("validate verification result: %w", err)
			}
			latestVerification = &verification
			if err := engine.events.Publish(ctx, event.VerificationCompleted{
				RunID: string(state.ID), TaskID: string(task.ID), Passed: verification.Passed(), EvidenceGaps: append([]string(nil), verification.EvidenceGaps...),
			}); err != nil {
				return DirectRunResult{}, fmt.Errorf("publish verification completed: %w", err)
			}
			if err := engine.transitionRun(ctx, &state, RunStatusReflectingTask); err != nil {
				return DirectRunResult{}, err
			}
			if err := engine.transitionTask(ctx, state.ID, task, TaskStatusReflecting); err != nil {
				return DirectRunResult{}, err
			}

			reflection, reflectErr := engine.reflector.Reflect(ctx, ReflectionInput{
				RunID: state.ID, Task: *task, Candidate: *outcome.Candidate, Verification: verification,
				Attempts: task.Attempts, Budget: state.Budget,
			})
			if reflectErr != nil {
				_ = engine.failDirect(ctx, &state, task, StopReasonProviderError)
				return DirectRunResult{State: state, Steps: steps, Verification: latestVerification, Reason: reflectErr.Error()}, reflectErr
			}
			if err := reflection.Validate(verification); err != nil {
				_ = engine.failDirect(ctx, &state, task, StopReasonProviderError)
				return DirectRunResult{State: state, Steps: steps, Verification: latestVerification, Reason: err.Error()}, fmt.Errorf("validate reflection result: %w", err)
			}
			latestReflection = &reflection
			state.Reflections = append(state.Reflections, reflection)
			if err := engine.events.Publish(ctx, event.ReflectionCompleted{
				RunID: string(state.ID), TaskID: string(task.ID), Scope: string(reflection.Scope), Verdict: string(reflection.Verdict),
			}); err != nil {
				return DirectRunResult{}, fmt.Errorf("publish reflection completed: %w", err)
			}

			switch reflection.Verdict {
			case ReflectionAccept:
				result := outcome.Candidate.Result
				result.EvidenceIDs = append([]EvidenceID(nil), result.EvidenceIDs...)
				task.Result = &result
				if err := engine.transitionTask(ctx, state.ID, task, TaskStatusCompleted); err != nil {
					return DirectRunResult{}, err
				}
				if err := engine.completeDirect(ctx, &state); err != nil {
					return DirectRunResult{}, err
				}
				finalMessage := outcome.Candidate.FinalMessage
				return DirectRunResult{State: state, Steps: steps, FinalMessage: &finalMessage, Verification: latestVerification, Reflection: latestReflection}, nil
			case ReflectionRetry:
				if task.Attempts >= engine.options.MaxAttempts {
					if err := engine.failDirect(ctx, &state, task, StopReasonVerificationFailed); err != nil {
						return DirectRunResult{}, err
					}
					return DirectRunResult{State: state, Steps: steps, Verification: latestVerification, Reflection: latestReflection, Reason: "task retry attempts exhausted"}, nil
				}
				if err := engine.transitionTask(ctx, state.ID, task, TaskStatusReady); err != nil {
					return DirectRunResult{}, err
				}
				if err := engine.transitionRun(ctx, &state, RunStatusTaskRunning); err != nil {
					return DirectRunResult{}, err
				}
				messages = appendRetryFeedback(messages, *outcome.Candidate, verification, reflection, engine.options.RetryPrompt)
			case ReflectionReplan:
				if err := engine.transitionTask(ctx, state.ID, task, TaskStatusBlocked); err != nil {
					return DirectRunResult{}, err
				}
				if err := engine.transitionRun(ctx, &state, RunStatusPlanning); err != nil {
					return DirectRunResult{}, err
				}
				return DirectRunResult{State: state, Steps: steps, Verification: latestVerification, Reflection: latestReflection, Reason: reflection.NextActionHint}, nil
			case ReflectionAskUser:
				if err := engine.transitionTask(ctx, state.ID, task, TaskStatusBlocked); err != nil {
					return DirectRunResult{}, err
				}
				if err := engine.transitionRun(ctx, &state, RunStatusSuspended); err != nil {
					return DirectRunResult{}, err
				}
				state.StopReason = StopReasonUserInputRequired
				return DirectRunResult{State: state, Steps: steps, Verification: latestVerification, Reflection: latestReflection, Reason: reflection.NextActionHint}, nil
			case ReflectionAbort:
				if err := engine.failDirect(ctx, &state, task, StopReasonVerificationFailed); err != nil {
					return DirectRunResult{}, err
				}
				return DirectRunResult{State: state, Steps: steps, Verification: latestVerification, Reflection: latestReflection, Reason: reflection.NextActionHint}, nil
			default:
				return DirectRunResult{}, fmt.Errorf("direct engine received unsupported reflection verdict %q", reflection.Verdict)
			}
		default:
			return DirectRunResult{}, fmt.Errorf("direct engine received unsupported task outcome %q", outcome.Kind)
		}
	}
}

func validateDirectStart(state RunState) error {
	if strings.TrimSpace(string(state.ID)) == "" {
		return errors.New("direct engine run ID is empty")
	}
	if state.Status != RunStatusInitialized {
		return fmt.Errorf("direct engine run status must be %q", RunStatusInitialized)
	}
	if state.Graph.Kind != ExecutionDirect || len(state.Graph.Tasks) != 1 {
		return errors.New("direct engine requires a direct graph with exactly one root task")
	}
	if state.Graph.Tasks[0].Status != TaskStatusPending {
		return fmt.Errorf("direct engine root task status must be %q", TaskStatusPending)
	}
	return state.Budget.Validate()
}

func (engine *DirectEngine) completeDirect(ctx context.Context, state *RunState) error {
	for _, next := range []RunStatus{RunStatusScheduling, RunStatusVerifyingRun, RunStatusReflectingFinal, RunStatusSynthesizing, RunStatusCompleted} {
		if err := engine.transitionRun(ctx, state, next); err != nil {
			return err
		}
	}
	state.StopReason = StopReasonCompleted
	state.ActiveTaskID = ""
	return nil
}

func (engine *DirectEngine) failDirect(ctx context.Context, state *RunState, task *Task, reason StopReason) error {
	if task.Status != TaskStatusFailed && task.Status != TaskStatusCompleted && task.Status != TaskStatusCancelled {
		if err := engine.transitionTask(context.WithoutCancel(ctx), state.ID, task, TaskStatusFailed); err != nil {
			return err
		}
	}
	if state.Status != RunStatusFailed && state.Status != RunStatusCompleted && state.Status != RunStatusCancelled {
		if err := engine.transitionRun(context.WithoutCancel(ctx), state, RunStatusFailed); err != nil {
			return err
		}
	}
	state.StopReason = reason
	return nil
}

func (engine *DirectEngine) cancelDirect(ctx context.Context, state *RunState, task *Task) error {
	publishCtx := context.WithoutCancel(ctx)
	if task.Status != TaskStatusCancelled && task.Status != TaskStatusCompleted && task.Status != TaskStatusFailed {
		if err := engine.transitionTask(publishCtx, state.ID, task, TaskStatusCancelled); err != nil {
			return err
		}
	}
	if state.Status != RunStatusCancelled && state.Status != RunStatusCompleted && state.Status != RunStatusFailed {
		if err := engine.transitionRun(publishCtx, state, RunStatusCancelled); err != nil {
			return err
		}
	}
	state.StopReason = StopReasonCancelled
	return nil
}

func (engine *DirectEngine) transitionRun(ctx context.Context, state *RunState, next RunStatus) error {
	previous := state.Status
	if err := state.Transition(next); err != nil {
		return err
	}
	if err := engine.events.Publish(ctx, event.EngineStatusChanged{
		RunID: string(state.ID), Entity: "run", EntityID: string(state.ID), From: string(previous), To: string(next),
	}); err != nil {
		return fmt.Errorf("publish run status changed: %w", err)
	}
	return nil
}

func (engine *DirectEngine) transitionTask(ctx context.Context, runID RunID, task *Task, next TaskStatus) error {
	previous := task.Status
	if err := task.Transition(next); err != nil {
		return err
	}
	if err := engine.events.Publish(ctx, event.EngineStatusChanged{
		RunID: string(runID), Entity: "task", EntityID: string(task.ID), From: string(previous), To: string(next),
	}); err != nil {
		return fmt.Errorf("publish task status changed: %w", err)
	}
	return nil
}

func appendRetryFeedback(messages []llm.Message, candidate CandidateTaskResult, verification Verification, reflection Reflection, retryPrompt string) []llm.Message {
	payload, _ := json.Marshal(struct {
		Verification Verification `json:"verification"`
		Reflection   Reflection   `json:"reflection"`
	}{Verification: verification, Reflection: reflection})
	result := cloneLLMMessages(messages)
	result = append(result, candidate.FinalMessage, llm.UserMessage(retryPrompt+"\n\nFeedback:\n"+string(payload)))
	return result
}

func cloneRunState(state RunState) (RunState, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return RunState{}, fmt.Errorf("clone direct run state: %w", err)
	}
	var cloned RunState
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return RunState{}, fmt.Errorf("clone direct run state: %w", err)
	}
	return cloned, nil
}

func cloneLLMMessages(messages []llm.Message) []llm.Message {
	cloned := make([]llm.Message, len(messages))
	copy(cloned, messages)
	for index := range cloned {
		cloned[index].ToolCalls = append([]llm.ToolCall(nil), cloned[index].ToolCalls...)
		for callIndex := range cloned[index].ToolCalls {
			cloned[index].ToolCalls[callIndex].Arguments = append([]byte(nil), cloned[index].ToolCalls[callIndex].Arguments...)
		}
	}
	return cloned
}

func mergeEvidence(existing, returned []Evidence) []Evidence {
	if len(returned) == 0 {
		return cloneEvidence(existing)
	}
	merged := cloneEvidence(existing)
	positions := make(map[EvidenceID]int, len(merged))
	for index, item := range merged {
		positions[item.ID] = index
	}
	for _, item := range cloneEvidence(returned) {
		if index, ok := positions[item.ID]; ok {
			merged[index] = item
			continue
		}
		positions[item.ID] = len(merged)
		merged = append(merged, item)
	}
	return merged
}

func mergeBudget(existing, returned BudgetState) BudgetState {
	if returned.Budget == (Budget{}) {
		returned.Budget = existing.Budget
	}
	return returned
}

func cloneEvidence(evidence []Evidence) []Evidence {
	cloned := make([]Evidence, len(evidence))
	copy(cloned, evidence)
	for index := range cloned {
		cloned[index].CriterionIDs = append([]string(nil), cloned[index].CriterionIDs...)
		if cloned[index].Artifact != nil {
			artifact := *cloned[index].Artifact
			cloned[index].Artifact = &artifact
		}
	}
	return cloned
}
