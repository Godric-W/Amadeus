package react

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type CallExecutor interface {
	ExecuteBatch(context.Context, []tool.ToolCall, tool.NormalizedCallRecorder) ([]tool.ToolExecution, error)
}

type ProgressObserver interface {
	Observe(ProgressSample) ([]ProgressSignal, error)
}

type RolloutRecorder interface {
	RecordToolCalls(context.Context, llm.Message) error
	RecordToolOutcomes(context.Context, []ToolOutcome) error
}

type RunnerOptions struct {
	Temperature      float64
	MaxParallelTools int
	Events           protocol.EventSink
	Rollout          RolloutRecorder
}

type Runner struct {
	think   ThinkPort
	analyze AnalyzePort
	act     ActPort
	observe ObservePort
	events  protocol.EventSink
	options RunnerOptions
	now     func() time.Time
}

type RunnerPhases struct {
	Think   ThinkPort
	Analyze AnalyzePort
	Act     ActPort
	Observe ObservePort
}

func NewRunner(iterator ModelIterator, executor CallExecutor, progress ProgressObserver, options RunnerOptions) (*Runner, error) {
	if iterator == nil {
		return nil, errors.New("Reactor model iterator is nil")
	}
	if executor == nil {
		return nil, errors.New("Reactor tool executor is nil")
	}
	if progress == nil {
		return nil, errors.New("Reactor progress observer is nil")
	}
	if options.Temperature < 0 || options.Temperature > 2 {
		return nil, errors.New("Reactor temperature must be between 0 and 2")
	}
	if options.MaxParallelTools <= 0 {
		options.MaxParallelTools = 1
	}
	return NewRunnerWithPhases(RunnerPhases{
		Think: newModelThinker(iterator), Analyze: newDefaultAnalyzer(),
		Act: &defaultActor{executor: executor}, Observe: &defaultObserver{progress: progress, now: time.Now},
	}, options)
}

func NewRunnerWithPhases(phases RunnerPhases, options RunnerOptions) (*Runner, error) {
	if phases.Think == nil || phases.Analyze == nil || phases.Act == nil || phases.Observe == nil {
		return nil, errors.New("Reactor phases must all be configured")
	}
	if options.Temperature < 0 || options.Temperature > 2 {
		return nil, errors.New("Reactor temperature must be between 0 and 2")
	}
	return &Runner{think: phases.Think, analyze: phases.Analyze, act: phases.Act, observe: phases.Observe, events: options.Events, options: options, now: time.Now}, nil
}

func (runner *Runner) Run(ctx context.Context, request Request) (Result, error) {
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	state := newLoopState(request)
	startedAt := runner.now()
	finish := func(result Result) (Result, error) {
		state.Budget.Elapsed += runner.durationSince(startedAt)
		if state.Budget.Budget.MaxDuration > 0 && state.Budget.Elapsed > state.Budget.Budget.MaxDuration {
			state.Budget.Elapsed = state.Budget.Budget.MaxDuration
		}
		result.Budget = state.Budget
		if result.StopReason != "" {
			return result, result.Validate()
		}
		return result, nil
	}

	runCtx := ctx
	cancel := func() {}
	if state.Budget.Budget.MaxDuration > 0 {
		remaining := state.Budget.Budget.MaxDuration - state.Budget.Elapsed
		if remaining <= 0 {
			return finish(budgetResult(state.Budget, LimitWallClock, int64(state.Budget.Elapsed), int64(state.Budget.Budget.MaxDuration)))
		}
		runCtx, cancel = context.WithTimeout(ctx, remaining)
	}
	defer cancel()

	for iterationIndex := len(state.Iterations); ; iterationIndex++ {
		if result, ok := contextResult(ctx, runCtx, state.Budget, state.Iterations, state.Usage); ok {
			return finish(result)
		}
		if result, ok := exhaustedBeforeThink(state.Budget, state.Iterations, state.Usage); ok {
			return finish(result)
		}
		llmCallID := iterationID(request, iterationIndex)
		iterationCtx := runCtx
		completeIteration := func(string, string) error { return nil }

		if request.BeforeSample != nil {
			if err := request.BeforeSample(iterationCtx, request.Context); err != nil {
				if publishErr := completeIteration("failed", err.Error()); publishErr != nil {
					err = errors.Join(err, publishErr)
				}
				return finish(Result{Iterations: state.Iterations, Usage: state.Usage, StopReason: StopFailed, Reason: err.Error()})
			}
		}
		snapshot := request.Context.ForPrompt(request.ModelInfo)
		messages := snapshot.Items
		availableTools := request.AvailableTools
		estimated := request.Context.EstimatePromptTokens(request.ModelInfo, llm.Prompt{
			BaseInstructions: request.BaseInstructions,
			Tools:            toolDefinitions(availableTools),
			OutputSchema:     request.OutputSchema,
		})
		contextWindow := request.ModelInfo.ContextWindow
		if len(messages) == 0 {
			err := errors.New("ContextManager produced an empty Prompt")
			if publishErr := completeIteration("failed", err.Error()); publishErr != nil {
				err = errors.Join(err, publishErr)
			}
			return finish(Result{Iterations: state.Iterations, Usage: state.Usage, StopReason: StopFailed, Reason: err.Error()})
		}
		if contextWindow > 0 && estimated+int64(request.ModelInfo.MaxOutputTokens) > contextWindow {
			err := fmt.Errorf("Prompt exceeds model context window after compaction: estimated input %d + max output %d > context window %d", estimated, request.ModelInfo.MaxOutputTokens, contextWindow)
			if publishErr := completeIteration("failed", err.Error()); publishErr != nil {
				err = errors.Join(err, publishErr)
			}
			return finish(Result{Iterations: state.Iterations, Usage: state.Usage, StopReason: StopFailed, Reason: err.Error()})
		}

		think, err := runner.think.Think(iterationCtx, ThinkInput{
			LLMCallID:         llmCallID,
			Messages:          messages,
			BaseInstructions:  request.BaseInstructions,
			AvailableTools:    availableTools,
			OutputSchema:      request.OutputSchema,
			ParallelToolCalls: request.ModelInfo.SupportsParallelToolCalls,
			Temperature:       runner.options.Temperature,
			MaxOutputTokens:   request.ModelInfo.MaxOutputTokens,
		})
		if err != nil {
			if publishErr := completeIteration("failed", err.Error()); publishErr != nil {
				err = errors.Join(err, publishErr)
			}
			if result, ok := contextResult(ctx, runCtx, state.Budget, state.Iterations, state.Usage); ok {
				return finish(result)
			}
			return finish(Result{Iterations: state.Iterations, Usage: state.Usage, StopReason: StopFailed, Reason: err.Error()})
		}
		state.Usage = addUsage(state.Usage, think.Response.Usage)
		request.Context.UpdateUsage(think.Response.Usage)
		state.Budget.IterationsUsed++
		state.Budget.InputTokensUsed += think.Response.Usage.InputTokens
		state.Budget.OutputTokensUsed += think.Response.Usage.OutputTokens

		analysis, err := runner.analyze.Analyze(AnalyzeInput{Think: think, AvailableTools: availableTools})
		if err != nil {
			if publishErr := completeIteration("failed", err.Error()); publishErr != nil {
				err = errors.Join(err, publishErr)
			}
			return finish(Result{Iterations: state.Iterations, Usage: state.Usage, StopReason: StopFailed, Reason: err.Error()})
		}
		var act ActOutput
		if analysis.Kind == AnalysisAct {
			if state.Budget.Budget.MaxToolCalls > 0 && state.Budget.ToolCallsUsed+len(analysis.Calls) > state.Budget.Budget.MaxToolCalls {
				state.Iterations = append(state.Iterations, runner.rejectedToolIteration(iterationIndex, analysis))
				result := budgetResult(state.Budget, LimitToolCalls, int64(state.Budget.ToolCallsUsed+len(analysis.Calls)), int64(state.Budget.Budget.MaxToolCalls))
				result.Iterations, result.Usage = state.Iterations, state.Usage
				if err := completeIteration("budget_exhausted", result.Reason); err != nil {
					return Result{}, err
				}
				return finish(result)
			}
			toolCtx := tool.WithRequestSnapshot(iterationCtx, request.RequestSnapshot)
			var recorder tool.NormalizedCallRecorder
			if runner.options.Rollout != nil {
				message := analysis.Response.Message
				recorder = func(recordCtx context.Context, calls []tool.ToolCall) error {
					message.ToolCalls = normalizedMessageToolCalls(message.ToolCalls, calls)
					return runner.options.Rollout.RecordToolCalls(recordCtx, message)
				}
			}
			act, err = runner.act.Act(toolCtx, ActInput{Calls: analysis.Calls, AvailableTools: availableTools, RecordCalls: recorder})
			if err != nil {
				if publishErr := completeIteration("failed", err.Error()); publishErr != nil {
					err = errors.Join(err, publishErr)
				}
				return Result{}, err
			}
			state.Budget.ToolCallsUsed += act.Attempted
		}
		if analysis.Kind != AnalysisFinal && runner.options.Rollout != nil {
			outcomes := act.Outcomes
			if analysis.Kind == AnalysisArgumentError {
				outcomes = analysis.ArgumentFailures
			}
			if err := runner.options.Rollout.RecordToolOutcomes(iterationCtx, outcomes); err != nil {
				if publishErr := completeIteration("failed", err.Error()); publishErr != nil {
					err = errors.Join(err, publishErr)
				}
				return finish(Result{Iterations: state.Iterations, Usage: state.Usage, StopReason: StopFailed, Reason: err.Error()})
			}
		}
		observed, err := runner.observe.Observe(ObserveInput{Index: iterationIndex, Analysis: analysis, Act: act, AvailableTools: availableTools})
		if err != nil {
			if publishErr := completeIteration("failed", err.Error()); publishErr != nil {
				err = errors.Join(err, publishErr)
			}
			return Result{}, err
		}
		iterationEventStatus := string(observed.Iteration.Status)
		iterationEventReason := ""
		if observed.BlockedReason != "" {
			iterationEventStatus = "blocked"
			iterationEventReason = observed.BlockedReason
		} else if observed.StalledReason != "" {
			iterationEventStatus = "stalled"
			iterationEventReason = observed.StalledReason
		}
		if err := completeIteration(iterationEventStatus, iterationEventReason); err != nil {
			return Result{}, err
		}
		state.Iterations = append(state.Iterations, observed.Iteration)
		if result, ok := contextResult(ctx, runCtx, state.Budget, state.Iterations, state.Usage); ok {
			return finish(result)
		}
		if observed.BlockedReason != "" {
			return finish(Result{Iterations: state.Iterations, Usage: state.Usage, StopReason: StopBlocked, Reason: observed.BlockedReason})
		}
		if observed.StalledReason != "" {
			return finish(Result{Iterations: state.Iterations, Usage: state.Usage, StopReason: StopStalled, Reason: observed.StalledReason})
		}
		if analysis.Kind == AnalysisFinal {
			if analysis.FinalMessage == nil {
				return Result{}, errors.New("final analysis has no final message")
			}
			message := *analysis.FinalMessage
			return finish(Result{FinalMessage: &message, Iterations: state.Iterations, Usage: state.Usage, StopReason: StopCompleted})
		}
		if runner.options.Rollout == nil {
			request.Context.Record(observed.Replay...)
		}
	}
}

func effectiveInputLimit(model llm.ModelInfo) int64 {
	model = model.Normalized()
	limit := model.AutoCompactTokenLimit
	if model.ContextWindow > 0 {
		windowLimit := model.ContextWindow - int64(model.MaxOutputTokens)
		if windowLimit < 0 {
			windowLimit = 0
		}
		if limit <= 0 || windowLimit < limit {
			limit = windowLimit
		}
	}
	return limit
}

func normalizedMessageToolCalls(original []llm.ToolCall, calls []tool.ToolCall) []llm.ToolCall {
	byID := make(map[string]tool.ToolCall, len(calls))
	for _, call := range calls {
		byID[call.ID] = call
	}
	result := make([]llm.ToolCall, len(original))
	for index, call := range original {
		result[index] = call
		if normalized, ok := byID[call.ID]; ok {
			result[index].Arguments = append([]byte(nil), normalized.Payload...)
		}
	}
	return result
}

func (runner *Runner) rejectedToolIteration(index int, analysis AnalyzeOutput) Iteration {
	startedAt := runner.now()
	completedAt := runner.now()
	return Iteration{Index: index, LLMCallID: analysis.LLMCallID, Intent: "tool_calls_budget_rejected", ToolCalls: append([]tool.ToolCall(nil), analysis.Calls...), Status: IterationFailed, StartedAt: startedAt, CompletedAt: &completedAt}
}

func (runner *Runner) durationSince(startedAt time.Time) time.Duration {
	finishedAt := runner.now()
	if finishedAt.Before(startedAt) {
		return 0
	}
	return finishedAt.Sub(startedAt)
}

func contextResult(parent, runCtx context.Context, budget BudgetState, iterations []Iteration, usage llm.Usage) (Result, bool) {
	if err := parent.Err(); err != nil {
		return Result{Iterations: iterations, Usage: usage, StopReason: StopInterrupted, Reason: err.Error()}, true
	}
	if budget.Budget.MaxDuration > 0 && runCtx.Err() != nil {
		result := budgetResult(budget, LimitWallClock, int64(budget.Budget.MaxDuration), int64(budget.Budget.MaxDuration))
		result.Iterations, result.Usage = iterations, usage
		return result, true
	}
	return Result{}, false
}

func exhaustedBeforeThink(budget BudgetState, iterations []Iteration, usage llm.Usage) (Result, bool) {
	checks := []struct {
		reached bool
		limit   LimitKind
		used    int64
		maximum int64
	}{
		{budget.Budget.MaxIterations > 0 && budget.IterationsUsed >= budget.Budget.MaxIterations, LimitIterations, int64(budget.IterationsUsed), int64(budget.Budget.MaxIterations)},
	}
	for _, check := range checks {
		if check.reached {
			result := budgetResult(budget, check.limit, check.used, check.maximum)
			result.Iterations, result.Usage = iterations, usage
			return result, true
		}
	}
	return Result{}, false
}

func budgetResult(budget BudgetState, limit LimitKind, used, maximum int64) Result {
	return Result{Budget: budget, StopReason: StopBudgetExhausted, Limit: &LimitReached{Limit: limit, Used: used, Maximum: maximum}, Reason: fmt.Sprintf("%s budget reached: used %d of %d", limit, used, maximum)}
}

func iterationID(request Request, index int) string {
	return fmt.Sprintf("%s/llm-%d", request.TurnID, index+1)
}

func addUsage(total, next llm.Usage) llm.Usage {
	total.InputTokens += next.InputTokens
	total.CachedInputTokens += next.CachedInputTokens
	total.OutputTokens += next.OutputTokens
	total.ReasoningTokens += next.ReasoningTokens
	total.TotalTokens += next.TotalTokens
	return total
}

func stalledSignal(signals []ProgressSignal) (ProgressSignal, bool) {
	for _, signal := range signals {
		if signal.RecommendPlan && signal.Kind != ProgressHighImpact {
			return signal, true
		}
	}
	return ProgressSignal{}, false
}
