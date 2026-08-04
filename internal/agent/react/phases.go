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
	LLMCallID       string
	Messages        []llm.Message
	AvailableTools  []tool.Spec
	Temperature     float64
	MaxOutputTokens int
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
	AvailableTools []tool.Spec
}

type AnalyzeOutput struct {
	Kind             AnalysisKind
	LLMCallID        string
	Response         llm.Response
	FinalMessage     *llm.Message
	Calls            []tool.Call
	ArgumentFailures []ToolExecution
}

type AnalyzePort interface {
	Analyze(AnalyzeInput) (AnalyzeOutput, error)
}

type ActInput struct {
	Calls          []tool.Call
	AvailableTools []tool.Spec
}

type ActOutput struct {
	Executions []ToolExecution
	Attempted  int
}

type ActPort interface {
	Act(context.Context, ActInput) (ActOutput, error)
}

type ObserveInput struct {
	Index          int
	Analysis       AnalyzeOutput
	Act            ActOutput
	EvidenceBefore int
	AvailableTools []tool.Spec
}

type ObserveOutput struct {
	Iteration     Iteration
	Evidence      []Evidence
	Replay        []llm.Message
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
			ID: input.LLMCallID, Messages: input.Messages, AvailableTools: input.AvailableTools,
			Temperature: input.Temperature, MaxOutputTokens: input.MaxOutputTokens,
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

type defaultAnalyzer struct {
	validator *tool.ArgumentValidator
}

func newDefaultAnalyzer() *defaultAnalyzer {
	return &defaultAnalyzer{validator: tool.NewArgumentValidator()}
}

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
		normalized, failures, err := analyzer.normalizeCalls(classified.ToolCalls, input.AvailableTools)
		if err != nil {
			output.Kind = AnalysisArgumentError
			output.Calls = append([]tool.Call(nil), classified.ToolCalls...)
			output.ArgumentFailures = failures
			output.Response.Message.ToolCalls = argumentErrorMessageToolCalls(output.Response.Message.ToolCalls)
			return output, nil
		}
		output.Kind = AnalysisAct
		output.Calls = normalized
		output.Response.Message.ToolCalls = normalizedMessageToolCalls(output.Response.Message.ToolCalls, normalized)
		return output, nil
	default:
		return AnalyzeOutput{}, fmt.Errorf("unsupported analysis kind %q", classified.Kind)
	}
}

func argumentErrorMessageToolCalls(calls []llm.ToolCall) []llm.ToolCall {
	result := make([]llm.ToolCall, len(calls))
	for index, call := range calls {
		result[index] = call
		result[index].Arguments = []byte(`{"_amadeus_argument_error":true}`)
	}
	return result
}

func (analyzer *defaultAnalyzer) normalizeCalls(calls []tool.Call, specs []tool.Spec) ([]tool.Call, []ToolExecution, error) {
	indexed := make(map[string]tool.Spec, len(specs))
	for _, spec := range specs {
		indexed[spec.Name] = spec
	}
	normalized := make([]tool.Call, len(calls))
	failures := make([]ToolExecution, len(calls))
	var combined error
	for index, call := range calls {
		spec, ok := indexed[call.Name]
		var arguments []byte
		var err error
		if !ok {
			err = fmt.Errorf("tool %q is not available", call.Name)
		} else {
			arguments, err = analyzer.validator.Validate(spec, call.Arguments)
		}
		if err == nil {
			normalized[index] = tool.NewCall(call.ID, call.Name, arguments)
			continue
		}
		combined = errors.Join(combined, err)
		failures[index] = argumentFailureExecution(call, err)
	}
	if combined == nil {
		return normalized, nil, nil
	}
	for index, call := range calls {
		if failures[index].Observation.CallID == "" {
			failures[index] = argumentFailureExecution(call, errors.New("tool call was not executed because another call had invalid arguments"))
		}
	}
	return nil, failures, combined
}

type defaultActor struct {
	resources *resourceExecutor
}

func (actor *defaultActor) Act(ctx context.Context, input ActInput) (ActOutput, error) {
	indexed, err := actor.resources.Execute(ctx, input.Calls, input.AvailableTools)
	if err != nil {
		return ActOutput{}, err
	}
	executions := make([]ToolExecution, 0, len(indexed))
	for _, item := range indexed {
		executions = append(executions, item.execution)
	}
	return ActOutput{Executions: executions, Attempted: len(indexed)}, nil
}

type defaultObserver struct {
	progress ProgressObserver
	now      func() time.Time
}

func (observer *defaultObserver) Observe(input ObserveInput) (ObserveOutput, error) {
	startedAt := observer.now()
	iteration := Iteration{
		Index: input.Index, LLMCallID: input.Analysis.LLMCallID, ToolCalls: append([]tool.Call(nil), input.Analysis.Calls...),
		Status: IterationRunning, StartedAt: startedAt,
	}
	var executions []ToolExecution
	switch input.Analysis.Kind {
	case AnalysisFinal:
		iteration.Intent = "final"
	case AnalysisArgumentError:
		iteration.Intent = "tool_argument_error"
		executions = input.Analysis.ArgumentFailures
	case AnalysisAct:
		iteration.Intent = "tool_calls"
		executions = input.Act.Executions
	default:
		return ObserveOutput{}, fmt.Errorf("unsupported observation analysis kind %q", input.Analysis.Kind)
	}
	output := ObserveOutput{Iteration: iteration}
	for _, execution := range executions {
		output.Iteration.Observations = append(output.Iteration.Observations, execution.Observation)
		output.Iteration.Evidence = append(output.Iteration.Evidence, execution.Evidence)
		output.Iteration.Evidence = append(output.Iteration.Evidence, execution.SupplementalEvidence...)
		output.Evidence = append(output.Evidence, execution.Evidence)
		output.Evidence = append(output.Evidence, execution.SupplementalEvidence...)
		if execution.Observation.Blocking && output.BlockedReason == "" {
			output.BlockedReason = execution.Observation.Error
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
			Calls: input.Analysis.Calls, Observations: output.Iteration.Observations,
			EvidenceBefore: input.EvidenceBefore, EvidenceAfter: input.EvidenceBefore + countVerified(output.Evidence), Specs: input.AvailableTools,
		})
		if err != nil {
			return ObserveOutput{}, err
		}
		if signal, ok := stalledSignal(signals); ok {
			output.StalledReason = signal.Reason
		}
	}
	replay, err := ReplayToolResults(input.Analysis.Response.Message, executions)
	if err != nil {
		return ObserveOutput{}, err
	}
	output.Replay = replay
	return output, nil
}
