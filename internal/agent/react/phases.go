package react

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type ThinkInput struct {
	LLMCallID         string
	Messages          []llm.Message
	BaseInstructions  llm.BaseInstructions
	AvailableTools    []tool.ToolSpec
	OutputSchema      llm.OutputSchema
	ParallelToolCalls bool
	Temperature       float64
	MaxOutputTokens   int
}

type ThinkOutput struct {
	LLMCallID string
	Response  llm.Response
}

type ThinkPort interface {
	Think(context.Context, ThinkInput) (ThinkOutput, error)
}

type AnalysisKind string

const (
	AnalysisFinal         AnalysisKind = "final"
	AnalysisAct           AnalysisKind = "act"
	AnalysisArgumentError AnalysisKind = "argument_error"
)

type AnalyzeInput struct {
	Think          ThinkOutput
	AvailableTools []tool.ToolSpec
}

type AnalyzeOutput struct {
	Kind             AnalysisKind
	LLMCallID        string
	Response         llm.Response
	FinalMessage     *llm.Message
	Calls            []tool.ToolCall
	ArgumentFailures []ToolOutcome
}

type AnalyzePort interface {
	Analyze(AnalyzeInput) (AnalyzeOutput, error)
}

type ActInput struct {
	Calls          []tool.ToolCall
	AvailableTools []tool.ToolSpec
	RecordCalls    tool.NormalizedCallRecorder
}

type ActOutput struct {
	Calls     []tool.ToolCall
	Outcomes  []ToolOutcome
	Attempted int
}

type ActPort interface {
	Act(context.Context, ActInput) (ActOutput, error)
}

type ObserveInput struct {
	Index          int
	Analysis       AnalyzeOutput
	Act            ActOutput
	AvailableTools []tool.ToolSpec
}

type ObserveOutput struct {
	Iteration     Iteration
	BlockedReason string
	StalledReason string
}

type ObservePort interface {
	Observe(ObserveInput) (ObserveOutput, error)
}

type modelThinker struct {
	iterator ModelIterator
	attempts int
	backoff  time.Duration
}

func newModelThinker(iterator ModelIterator) *modelThinker {
	return &modelThinker{iterator: iterator, attempts: 2, backoff: 10 * time.Millisecond}
}

func (thinker *modelThinker) Think(ctx context.Context, input ThinkInput) (ThinkOutput, error) {
	var lastErr error
	for attempt := 0; attempt < thinker.attempts; attempt++ {
		iteration, err := thinker.iterator.Run(ctx, IterationInput{
			ID: input.LLMCallID, Messages: input.Messages, BaseInstructions: input.BaseInstructions,
			AvailableTools: input.AvailableTools, OutputSchema: input.OutputSchema,
			ParallelToolCalls: input.ParallelToolCalls, Temperature: input.Temperature,
			MaxOutputTokens: input.MaxOutputTokens,
		})
		if err == nil {
			return ThinkOutput{LLMCallID: input.LLMCallID, Response: iteration.Response}, nil
		}
		lastErr = err
		if !retryableThinkError(err) || attempt+1 == thinker.attempts {
			break
		}
		timer := time.NewTimer(thinker.backoff * time.Duration(attempt+1))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ThinkOutput{}, ctx.Err()
		case <-timer.C:
		}
	}
	return ThinkOutput{}, lastErr
}

func retryableThinkError(err error) bool {
	var providerErr *llm.ProviderError
	if !errors.As(err, &providerErr) {
		return false
	}
	switch providerErr.Kind {
	case llm.ProviderErrorNetwork, llm.ProviderErrorRateLimit, llm.ProviderErrorTimeout, llm.ProviderErrorUnavailable:
		return true
	case llm.ProviderErrorProtocol:
		return providerErr.Message == "model iteration returned neither text nor tool calls"
	default:
		return false
	}
}

type defaultAnalyzer struct{}

func newDefaultAnalyzer() *defaultAnalyzer { return &defaultAnalyzer{} }

func (analyzer *defaultAnalyzer) Analyze(input AnalyzeInput) (AnalyzeOutput, error) {
	classified, err := classify(input.Think.Response)
	if err != nil {
		return AnalyzeOutput{}, err
	}
	output := AnalyzeOutput{LLMCallID: input.Think.LLMCallID, Response: classified.Response}
	switch classified.Kind {
	case IterationCandidate:
		output.Kind = AnalysisFinal
		output.FinalMessage = classified.Candidate
		return output, nil
	case IterationToolCalls:
		output.Kind = AnalysisAct
		output.Calls = append([]tool.ToolCall(nil), classified.ToolCalls...)
		return output, nil
	default:
		return AnalyzeOutput{}, fmt.Errorf("unsupported analysis kind %q", classified.Kind)
	}
}

type defaultActor struct {
	executor CallExecutor
}

func (actor *defaultActor) Act(ctx context.Context, input ActInput) (ActOutput, error) {
	executions, err := actor.executor.ExecuteBatch(ctx, input.Calls, input.RecordCalls)
	if err != nil {
		return ActOutput{}, err
	}
	outcomes := make([]ToolOutcome, 0, len(executions))
	calls := make([]tool.ToolCall, 0, len(executions))
	for _, execution := range executions {
		calls = append(calls, execution.Call.Clone())
		outcomes = append(outcomes, projectToolExecution(execution))
	}
	return ActOutput{Calls: calls, Outcomes: outcomes, Attempted: len(executions)}, nil
}

type defaultObserver struct {
	progress ProgressObserver
	now      func() time.Time
}

func (observer *defaultObserver) Observe(input ObserveInput) (ObserveOutput, error) {
	startedAt := observer.now()
	calls := input.Analysis.Calls
	response := input.Analysis.Response.Message
	response.Parts = append([]llm.ContentPart(nil), response.Parts...)
	if input.Analysis.Kind == AnalysisAct && len(input.Act.Calls) == len(input.Analysis.Calls) {
		calls = input.Act.Calls
		response.ToolCalls = make([]llm.ToolCall, 0, len(calls))
		for _, call := range calls {
			response.ToolCalls = append(response.ToolCalls, llm.ToolCall{ID: call.ID, Name: call.Name, Arguments: append([]byte(nil), call.Payload...)})
		}
	}
	iteration := Iteration{
		Index: input.Index, LLMCallID: input.Analysis.LLMCallID, ToolCalls: append([]tool.ToolCall(nil), calls...),
		Status: IterationRunning, StartedAt: startedAt,
	}
	var outcomes []ToolOutcome
	switch input.Analysis.Kind {
	case AnalysisFinal:
		iteration.Intent = "final"
	case AnalysisArgumentError:
		iteration.Intent = "tool_argument_error"
		outcomes = input.Analysis.ArgumentFailures
	case AnalysisAct:
		iteration.Intent = "tool_calls"
		outcomes = input.Act.Outcomes
	default:
		return ObserveOutput{}, fmt.Errorf("unsupported observation analysis kind %q", input.Analysis.Kind)
	}
	output := ObserveOutput{Iteration: iteration}
	for _, outcome := range outcomes {
		output.Iteration.Outcomes = append(output.Iteration.Outcomes, outcome)
		if outcome.Blocking && output.BlockedReason == "" {
			output.BlockedReason = outcome.ErrorMessage()
		}
	}
	completedAt := observer.now()
	output.Iteration.CompletedAt = &completedAt
	output.Iteration.Status = IterationCompleted
	if input.Analysis.Kind == AnalysisFinal {
		return output, nil
	}
	if input.Analysis.Kind == AnalysisAct {
		signals, err := observer.progress.Observe(ProgressSample{
			Calls: calls, Outcomes: output.Iteration.Outcomes, Specs: input.AvailableTools,
		})
		if err != nil {
			return ObserveOutput{}, err
		}
		if signal, ok := stalledSignal(signals); ok {
			output.StalledReason = signal.Reason
		}
	}
	return output, nil
}
