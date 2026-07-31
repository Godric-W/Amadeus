package react

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type CallExecutor interface {
	Execute(context.Context, tool.Call) (ToolExecution, error)
}

type ProgressObserver interface {
	Observe(ProgressSample) ([]ProgressSignal, error)
}

type RunnerOptions struct {
	Temperature        float64
	MaxOutputTokens    int
	MaxParallelTools   int
	EscalateHighImpact bool
}

type Runner struct {
	iterator  ModelIterator
	executor  CallExecutor
	progress  ProgressObserver
	resources *resourceExecutor
	options   RunnerOptions
	now       func() time.Time
}

func NewRunner(iterator ModelIterator, executor CallExecutor, progress ProgressObserver, options RunnerOptions) (*Runner, error) {
	if iterator == nil {
		return nil, errors.New("ReAct runner model iterator is nil")
	}
	if executor == nil {
		return nil, errors.New("ReAct runner tool executor is nil")
	}
	if progress == nil {
		return nil, errors.New("ReAct runner progress observer is nil")
	}
	if options.Temperature < 0 || options.Temperature > 2 {
		return nil, errors.New("ReAct runner temperature must be between 0 and 2")
	}
	if options.MaxOutputTokens <= 0 {
		return nil, errors.New("ReAct runner max output tokens must be greater than zero")
	}
	if options.MaxParallelTools <= 0 {
		options.MaxParallelTools = 1
	}
	resources, err := newResourceExecutor(executor, options.MaxParallelTools)
	if err != nil {
		return nil, err
	}
	return &Runner{iterator: iterator, executor: executor, progress: progress, resources: resources, options: options, now: time.Now}, nil
}

func (runner *Runner) Run(ctx context.Context, input engine.TaskRunInput) (engine.TaskOutcome, error) {
	if err := input.Validate(); err != nil {
		return engine.TaskOutcome{}, err
	}
	budget := input.Budget
	if budget.Budget == (engine.Budget{}) {
		budget.Budget = input.Task.Budget
	}
	startedAt := time.Now()
	finish := func(outcome engine.TaskOutcome) engine.TaskOutcome {
		budget.Elapsed += time.Since(startedAt)
		if budget.Budget.MaxDuration > 0 && budget.Elapsed > budget.Budget.MaxDuration {
			budget.Elapsed = budget.Budget.MaxDuration
		}
		outcome.Budget = budget
		return outcome
	}

	runCtx := ctx
	cancel := func() {}
	if budget.Budget.MaxDuration > 0 {
		remaining := budget.Budget.MaxDuration - budget.Elapsed
		if remaining <= 0 {
			return finish(budgetFailure(budget, engine.BudgetLimitWallClock, int64(budget.Elapsed), int64(budget.Budget.MaxDuration))), nil
		}
		runCtx, cancel = context.WithTimeout(ctx, remaining)
	}
	defer cancel()

	messages := append([]llm.Message(nil), input.Messages...)
	if len(messages) == 0 {
		messages = append(messages, llm.UserMessage(input.Task.Objective))
	}

	steps := make([]engine.Step, 0)
	evidence := append([]engine.Evidence(nil), input.Evidence...)
	var usage llm.Usage
	for iterationIndex := 0; ; iterationIndex++ {
		if outcome, ok := contextOutcome(ctx, runCtx, budget, steps, evidence); ok {
			return finish(outcome), nil
		}
		if outcome, ok := exhaustedBeforeModelCall(budget, steps, evidence); ok {
			return finish(outcome), nil
		}
		iteration, err := runner.iterator.Run(runCtx, IterationInput{
			ID:              iterationID(input, iterationIndex),
			Messages:        messages,
			AvailableTools:  input.AvailableTools,
			Temperature:     runner.options.Temperature,
			MaxOutputTokens: maxOutputTokens(runner.options.MaxOutputTokens, budget),
		})
		if err != nil {
			if outcome, ok := contextOutcome(ctx, runCtx, budget, steps, evidence); ok {
				return finish(outcome), nil
			}
			return finish(engine.TaskOutcome{
				Kind: engine.TaskOutcomeFailed, Steps: steps, Evidence: evidence,
				StopReason: engine.StopReasonProviderError, Reason: err.Error(),
			}), nil
		}
		usage = addUsage(usage, iteration.Response.Usage)
		budget.StepsUsed++
		budget.InputTokensUsed += iteration.Response.Usage.InputTokens
		budget.OutputTokensUsed += iteration.Response.Usage.OutputTokens
		if outcome, ok := exceededAfterModelCall(budget, steps, evidence); ok {
			return finish(outcome), nil
		}

		switch iteration.Kind {
		case IterationCandidate:
			if iteration.Candidate == nil {
				return engine.TaskOutcome{}, errors.New("candidate model iteration has no candidate result")
			}
			step := runner.candidateStep(len(input.PriorSteps)+len(steps), iteration.Response)
			steps = append(steps, step)
			result := *iteration.Candidate
			result.EvidenceIDs = evidenceIDs(evidence)
			outcome := finish(engine.TaskOutcome{
				Kind:      engine.TaskOutcomeCandidateComplete,
				Candidate: &engine.CandidateTaskResult{Result: result, FinalMessage: iteration.Response.Message, Usage: usage},
				Steps:     steps, Evidence: evidence,
			})
			return outcome, outcome.Validate()
		case IterationToolCalls:
			if len(iteration.ToolCalls) == 0 {
				return engine.TaskOutcome{}, errors.New("tool_calls model iteration has no tool calls")
			}
			if budget.Budget.MaxToolCalls > 0 && budget.ToolCallsUsed+len(iteration.ToolCalls) > budget.Budget.MaxToolCalls {
				step := runner.rejectedToolStep(len(input.PriorSteps)+len(steps), iteration)
				steps = append(steps, step)
				outcome := budgetFailure(
					budget,
					engine.BudgetLimitToolCalls,
					int64(budget.ToolCallsUsed+len(iteration.ToolCalls)),
					int64(budget.Budget.MaxToolCalls),
				)
				outcome.Steps = steps
				outcome.Evidence = evidence
				return finish(outcome), nil
			}
			step, executions, attempted, err := runner.executeCalls(runCtx, len(input.PriorSteps)+len(steps), iteration, input.AvailableTools)
			if err != nil {
				return engine.TaskOutcome{}, err
			}
			budget.ToolCallsUsed += attempted
			steps = append(steps, step)
			for _, execution := range executions {
				evidence = append(evidence, execution.Evidence)
			}
			if outcome, ok := contextOutcome(ctx, runCtx, budget, steps, evidence); ok {
				return finish(outcome), nil
			}

			signals, err := runner.progress.Observe(ProgressSample{
				Calls: iteration.ToolCalls, Observations: step.Observations,
				EvidenceBefore: countVerified(evidence) - countVerified(step.Evidence),
				EvidenceAfter:  countVerified(evidence), Specs: input.AvailableTools,
			})
			if err != nil {
				return engine.TaskOutcome{}, err
			}
			if signal, ok := needsPlanSignal(signals, runner.options.EscalateHighImpact); ok {
				return finish(engine.TaskOutcome{Kind: engine.TaskOutcomeNeedsPlan, Steps: steps, Evidence: evidence, Reason: signal.Reason}), nil
			}
			replay, err := ReplayToolResults(iteration.Response.Message, executions)
			if err != nil {
				return engine.TaskOutcome{}, err
			}
			messages = append(messages, replay...)
		default:
			return engine.TaskOutcome{}, fmt.Errorf("unsupported model iteration kind %q", iteration.Kind)
		}
	}
}

func (runner *Runner) executeCalls(ctx context.Context, index int, iteration IterationResult, specs []tool.Spec) (engine.Step, []ToolExecution, int, error) {
	startedAt := runner.now()
	step := engine.Step{
		Index: index, Decision: engine.DecisionSummary{Intent: "tool_calls", NextAction: "execute requested tools"},
		ToolCalls: append([]tool.Call(nil), iteration.ToolCalls...), Status: engine.StepStatusRunning, StartedAt: startedAt,
	}
	indexedExecutions, err := runner.resources.Execute(ctx, iteration.ToolCalls, specs)
	if err != nil {
		return engine.Step{}, nil, 0, err
	}
	executions := make([]ToolExecution, 0, len(indexedExecutions))
	for _, indexed := range indexedExecutions {
		execution := indexed.execution
		executions = append(executions, execution)
		step.Observations = append(step.Observations, execution.Observation)
		step.Evidence = append(step.Evidence, execution.Evidence)
	}
	if ctx.Err() != nil {
		step.Status = engine.StepStatusCancelled
		completedAt := runner.now()
		step.CompletedAt = &completedAt
		return step, executions, len(indexedExecutions), nil
	}
	step.Status = engine.StepStatusCompleted
	completedAt := runner.now()
	step.CompletedAt = &completedAt
	return step, executions, len(indexedExecutions), nil
}

func (runner *Runner) rejectedToolStep(index int, iteration IterationResult) engine.Step {
	startedAt := runner.now()
	completedAt := runner.now()
	return engine.Step{
		Index:     index,
		Decision:  engine.DecisionSummary{Intent: "tool_calls", NextAction: "stop before tool execution: budget exceeded"},
		ToolCalls: append([]tool.Call(nil), iteration.ToolCalls...), Status: engine.StepStatusFailed,
		StartedAt: startedAt, CompletedAt: &completedAt,
	}
}

func (runner *Runner) candidateStep(index int, response llm.Response) engine.Step {
	startedAt := runner.now()
	completedAt := runner.now()
	return engine.Step{
		Index: index, Decision: engine.DecisionSummary{Intent: "candidate_complete", NextAction: "verify candidate result"},
		Status: engine.StepStatusCompleted, StartedAt: startedAt, CompletedAt: &completedAt,
	}
}

func cancelled(steps []engine.Step, evidence []engine.Evidence, err error) engine.TaskOutcome {
	return engine.TaskOutcome{
		Kind: engine.TaskOutcomeCancelled, Steps: steps, Evidence: evidence,
		StopReason: engine.StopReasonCancelled, Reason: err.Error(),
	}
}

func contextOutcome(parent, runCtx context.Context, budget engine.BudgetState, steps []engine.Step, evidence []engine.Evidence) (engine.TaskOutcome, bool) {
	if err := parent.Err(); err != nil {
		return cancelled(steps, evidence, err), true
	}
	if budget.Budget.MaxDuration > 0 && runCtx.Err() != nil {
		outcome := budgetFailure(budget, engine.BudgetLimitWallClock, int64(budget.Budget.MaxDuration), int64(budget.Budget.MaxDuration))
		outcome.Steps = steps
		outcome.Evidence = evidence
		return outcome, true
	}
	return engine.TaskOutcome{}, false
}

func exhaustedBeforeModelCall(budget engine.BudgetState, steps []engine.Step, evidence []engine.Evidence) (engine.TaskOutcome, bool) {
	checks := []struct {
		reached bool
		limit   engine.BudgetLimit
		used    int64
		maximum int64
	}{
		{budget.Budget.MaxSteps > 0 && budget.StepsUsed >= budget.Budget.MaxSteps, engine.BudgetLimitSteps, int64(budget.StepsUsed), int64(budget.Budget.MaxSteps)},
		{budget.Budget.MaxInputTokens > 0 && budget.InputTokensUsed >= budget.Budget.MaxInputTokens, engine.BudgetLimitInputTokens, budget.InputTokensUsed, budget.Budget.MaxInputTokens},
		{budget.Budget.MaxOutputTokens > 0 && budget.OutputTokensUsed >= budget.Budget.MaxOutputTokens, engine.BudgetLimitOutputTokens, budget.OutputTokensUsed, budget.Budget.MaxOutputTokens},
	}
	for _, check := range checks {
		if !check.reached {
			continue
		}
		outcome := budgetFailure(budget, check.limit, check.used, check.maximum)
		outcome.Steps = steps
		outcome.Evidence = evidence
		return outcome, true
	}
	return engine.TaskOutcome{}, false
}

func exceededAfterModelCall(budget engine.BudgetState, steps []engine.Step, evidence []engine.Evidence) (engine.TaskOutcome, bool) {
	checks := []struct {
		exceeded bool
		limit    engine.BudgetLimit
		used     int64
		maximum  int64
	}{
		{budget.Budget.MaxInputTokens > 0 && budget.InputTokensUsed > budget.Budget.MaxInputTokens, engine.BudgetLimitInputTokens, budget.InputTokensUsed, budget.Budget.MaxInputTokens},
		{budget.Budget.MaxOutputTokens > 0 && budget.OutputTokensUsed > budget.Budget.MaxOutputTokens, engine.BudgetLimitOutputTokens, budget.OutputTokensUsed, budget.Budget.MaxOutputTokens},
	}
	for _, check := range checks {
		if !check.exceeded {
			continue
		}
		outcome := budgetFailure(budget, check.limit, check.used, check.maximum)
		outcome.Steps = steps
		outcome.Evidence = evidence
		return outcome, true
	}
	return engine.TaskOutcome{}, false
}

func budgetFailure(budget engine.BudgetState, limit engine.BudgetLimit, used, maximum int64) engine.TaskOutcome {
	stopReason := engine.StopReasonBudgetExceeded
	if limit == engine.BudgetLimitSteps {
		stopReason = engine.StopReasonMaxSteps
	}
	return engine.TaskOutcome{
		Kind:       engine.TaskOutcomeFailed,
		Budget:     budget,
		StopReason: stopReason,
		Limit:      &engine.LimitReached{Limit: limit, Used: used, Maximum: maximum},
		Reason:     fmt.Sprintf("%s budget reached: used %d of %d", limit, used, maximum),
	}
}

func maxOutputTokens(configured int, budget engine.BudgetState) int {
	if budget.Budget.MaxOutputTokens <= 0 {
		return configured
	}
	remaining := budget.Budget.MaxOutputTokens - budget.OutputTokensUsed
	if remaining < int64(configured) {
		return int(remaining)
	}
	return configured
}

func iterationID(input engine.TaskRunInput, index int) string {
	return fmt.Sprintf("%s/%s/iteration-%d", input.RunID, input.Task.ID, index+1)
}

func evidenceIDs(evidence []engine.Evidence) []engine.EvidenceID {
	ids := make([]engine.EvidenceID, 0, len(evidence))
	for _, item := range evidence {
		if item.Verified {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func countVerified(evidence []engine.Evidence) int {
	count := 0
	for _, item := range evidence {
		if item.Verified {
			count++
		}
	}
	return count
}

func addUsage(total, next llm.Usage) llm.Usage {
	total.InputTokens += next.InputTokens
	total.CachedInputTokens += next.CachedInputTokens
	total.OutputTokens += next.OutputTokens
	total.ReasoningTokens += next.ReasoningTokens
	total.TotalTokens += next.TotalTokens
	return total
}

func needsPlanSignal(signals []ProgressSignal, escalateHighImpact bool) (ProgressSignal, bool) {
	for _, signal := range signals {
		if !signal.RecommendPlan {
			continue
		}
		if signal.Kind == ProgressHighImpact && !escalateHighImpact {
			continue
		}
		return signal, true
	}
	return ProgressSignal{}, false
}

var _ engine.ReActRunner = (*Runner)(nil)
