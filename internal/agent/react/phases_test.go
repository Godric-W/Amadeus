package react

import (
	"context"
	"reflect"
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type recordingThink struct{ order *[]string }

func (phase recordingThink) Think(context.Context, ThinkInput) (ThinkOutput, error) {
	*phase.order = append(*phase.order, "think")
	return ThinkOutput{LLMCallID: "call-1", Response: llm.Response{Message: llm.AssistantMessage("done"), FinishReason: llm.FinishReasonStop}}, nil
}

type recordingAnalyze struct{ order *[]string }

func (phase recordingAnalyze) Analyze(input AnalyzeInput) (AnalyzeOutput, error) {
	*phase.order = append(*phase.order, "analyze")
	message := input.Think.Response.Message
	return AnalyzeOutput{Kind: AnalysisFinal, LLMCallID: input.Think.LLMCallID, Response: input.Think.Response, FinalMessage: &message}, nil
}

type recordingAct struct{ order *[]string }

func (phase recordingAct) Act(context.Context, ActInput) (ActOutput, error) {
	*phase.order = append(*phase.order, "act")
	return ActOutput{}, nil
}

type recordingObserve struct{ order *[]string }

func (phase recordingObserve) Observe(input ObserveInput) (ObserveOutput, error) {
	*phase.order = append(*phase.order, "observe")
	return ObserveOutput{Iteration: Iteration{Index: input.Index, LLMCallID: input.Analysis.LLMCallID, Intent: "final", Status: IterationCompleted}}, nil
}

func TestRunnerPhasesAreIndependentlyInjectableAndOrdered(t *testing.T) {
	order := []string{}
	runner, err := NewRunnerWithPhases(RunnerPhases{
		Think: recordingThink{order: &order}, Analyze: recordingAnalyze{order: &order},
		Act: recordingAct{order: &order}, Observe: recordingObserve{order: &order},
	}, RunnerOptions{MaxOutputTokens: 128})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"think", "analyze", "observe"}) || result.StopReason != StopCompleted {
		t.Fatalf("unexpected phase order/result: order=%v result=%#v", order, result)
	}
}

func TestDefaultAnalyzerLeavesToolArgumentsForRouter(t *testing.T) {
	response := llm.Response{Message: llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call-1", Name: "read_file", Arguments: []byte(`{"path":"README.md",`)}), FinishReason: llm.FinishReasonToolCalls}
	analysis, err := newDefaultAnalyzer().Analyze(AnalyzeInput{
		Think:          ThinkOutput{LLMCallID: "llm-1", Response: response},
		AvailableTools: []tool.Spec{{Name: "read_file", InputSchema: []byte(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Kind != AnalysisAct || len(analysis.Calls) != 1 || string(analysis.Calls[0].Payload) != `{"path":"README.md",` {
		t.Fatalf("Analyzer changed Tool arguments before Router validation: %#v", analysis)
	}
}

func TestModelThinkerRetriesTransientProviderFailure(t *testing.T) {
	message := llm.AssistantMessage("recovered")
	iterator := &scriptedIterator{
		results: []IterationResult{{}, {Kind: IterationCandidate, Response: llm.Response{Message: message, FinishReason: llm.FinishReasonStop}, Candidate: &message}},
		errors:  []error{&llm.ProviderError{Kind: llm.ProviderErrorUnavailable, Message: "temporary"}, nil},
	}
	output, err := newModelThinker(iterator).Think(context.Background(), ThinkInput{LLMCallID: "llm-1", Messages: []llm.Message{llm.UserMessage("hi")}, MaxOutputTokens: 128})
	if err != nil {
		t.Fatal(err)
	}
	if len(iterator.inputs) != 2 || output.Response.Message.Content != "recovered" {
		t.Fatalf("transient failure was not retried: calls=%d output=%#v", len(iterator.inputs), output)
	}
}

func TestModelThinkerDoesNotRetryInvalidRequest(t *testing.T) {
	iterator := &scriptedIterator{errors: []error{&llm.ProviderError{Kind: llm.ProviderErrorInvalidRequest, Message: "bad request"}}}
	_, err := newModelThinker(iterator).Think(context.Background(), ThinkInput{LLMCallID: "llm-1", Messages: []llm.Message{llm.UserMessage("hi")}, MaxOutputTokens: 128})
	if err == nil || len(iterator.inputs) != 1 {
		t.Fatalf("invalid request retry behavior is wrong: calls=%d err=%v", len(iterator.inputs), err)
	}
}

func TestNewRunnerWithPhasesRejectsMissingPort(t *testing.T) {
	_, err := NewRunnerWithPhases(RunnerPhases{Think: recordingThink{order: &[]string{}}}, RunnerOptions{MaxOutputTokens: 128})
	if err == nil {
		t.Fatal("missing phases were accepted")
	}
}
