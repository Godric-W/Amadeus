package react

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type fakeClient struct {
	model   llm.ModelInfo
	stream  llm.Stream
	request llm.Request
	err     error
}

func (client *fakeClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unexpected Complete call")
}

func (client *fakeClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.request = request
	return client.stream, client.err
}

func (client *fakeClient) Model() llm.ModelInfo { return client.model }
func (*fakeClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

type fakeStream struct {
	chunks []llm.StreamChunk
	next   int
	closed bool
}

func (stream *fakeStream) Recv() (llm.StreamChunk, error) {
	if stream.next >= len(stream.chunks) {
		return llm.StreamChunk{}, io.EOF
	}
	chunk := stream.chunks[stream.next]
	stream.next++
	return chunk, nil
}

func (stream *fakeStream) Close() error {
	stream.closed = true
	return nil
}

func TestIteratorProducesCandidateAndStableEvents(t *testing.T) {
	usage := llm.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}
	stream := &fakeStream{chunks: []llm.StreamChunk{
		{ID: "response_1", ReasoningDelta: "think"},
		{ID: "response_1", ContentDelta: "hel"},
		{ID: "response_1", ContentDelta: "lo"},
		{ID: "response_1", RequestID: "request_1", FinishReason: llm.FinishReasonStop, ProviderFinishReason: "stop", Usage: &usage},
	}}
	client := &fakeClient{model: llm.ModelInfo{Provider: "fake", Name: "fake-model"}, stream: stream}
	sink := event.NewMemorySink()
	iterator, err := NewIterator(client, sink)
	if err != nil {
		t.Fatalf("create iterator: %v", err)
	}
	input := validIterationInput()
	input.AvailableTools = []tool.ToolSpec{{Name: "read", Description: "Read a file", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	input.OutputSchema = llm.OutputSchema(`{"type":"object","properties":{"answer":{"type":"string"}}}`)

	result, err := iterator.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("run iteration: %v", err)
	}
	if result.Kind != IterationCandidate || result.Candidate == nil || result.Candidate.Content != "hello" {
		t.Fatalf("unexpected candidate result: %#v", result)
	}
	if !stream.closed || client.request.Model != "fake-model" || len(client.request.Prompt.Tools) != 1 || client.request.Prompt.Tools[0].Name != "read" {
		t.Fatalf("unexpected model request or stream state: request=%#v closed=%v", client.request, stream.closed)
	}
	if client.request.Prompt.BaseInstructions.Text != "stable Agent protocol" || len(client.request.Prompt.Input) != 1 || client.request.Prompt.Input[0].Content != "inspect repository" {
		t.Fatalf("model request omitted stable Agent protocol: %#v", client.request.Prompt)
	}
	if string(client.request.Prompt.OutputSchema) != string(input.OutputSchema) {
		t.Fatalf("model request omitted OutputSchema: %#v", client.request.Prompt.OutputSchema)
	}
	expectedTypes := []event.Type{event.TypeLLMCallStarted, event.TypeReasoningDelta, event.TypeTextDelta, event.TypeTextDelta, event.TypeUsageUpdated, event.TypeLLMCallCompleted}
	assertEventTypes(t, sink.Snapshot(), expectedTypes)
}

func TestIteratorPreservesAssembledSystemPrompt(t *testing.T) {
	stream := &fakeStream{chunks: []llm.StreamChunk{{ContentDelta: "done", FinishReason: llm.FinishReasonStop}}}
	client := &fakeClient{model: llm.ModelInfo{Provider: "fake", Name: "fake-model"}, stream: stream}
	iterator, err := NewIterator(client, event.NewMemorySink())
	if err != nil {
		t.Fatalf("create iterator: %v", err)
	}
	input := validIterationInput()
	input.BaseInstructions = llm.BaseInstructions{Text: "assembled context"}
	input.Messages = []llm.Message{llm.UserMessage("inspect repository")}

	if _, err := iterator.Run(context.Background(), input); err != nil {
		t.Fatalf("run iteration: %v", err)
	}
	if client.request.Prompt.BaseInstructions.Text != "assembled context" || !reflect.DeepEqual(client.request.Prompt.Input, input.Messages) {
		t.Fatalf("iterator replaced assembled Prompt: got %#v, want %#v", client.request.Prompt, input)
	}
}

func TestIteratorUsesInputBaseInstructions(t *testing.T) {
	stream := &fakeStream{chunks: []llm.StreamChunk{{ContentDelta: "done", FinishReason: llm.FinishReasonStop}}}
	client := &fakeClient{model: llm.ModelInfo{Provider: "fake", Name: "fake-model"}, stream: stream}
	iterator, err := NewIterator(client, event.NewMemorySink())
	if err != nil {
		t.Fatalf("create configured iterator: %v", err)
	}
	input := validIterationInput()
	input.BaseInstructions = llm.BaseInstructions{Text: " assembled protocol "}
	if _, err := iterator.Run(context.Background(), input); err != nil {
		t.Fatalf("run configured iterator: %v", err)
	}
	if client.request.Prompt.BaseInstructions.Text != " assembled protocol " || len(client.request.Prompt.Input) != 1 {
		t.Fatalf("iterator omitted configured system prompt: %#v", client.request.Prompt)
	}
}

func TestIteratorRejectsMissingBaseInstructionsAtRunBoundary(t *testing.T) {
	client := &fakeClient{model: llm.ModelInfo{Provider: "fake", Name: "fake-model"}}
	iterator, err := NewIterator(client, event.NewMemorySink())
	if err != nil {
		t.Fatal(err)
	}
	input := validIterationInput()
	input.BaseInstructions = llm.BaseInstructions{}
	if _, err := iterator.Run(context.Background(), input); err == nil || !strings.Contains(err.Error(), "BaseInstructions") {
		t.Fatalf("unexpected missing BaseInstructions error: %v", err)
	}
}

func TestIteratorProducesNormalizedToolCalls(t *testing.T) {
	stream := &fakeStream{chunks: []llm.StreamChunk{{
		ID: "response_tools", FinishReason: llm.FinishReasonToolCalls, ProviderFinishReason: "tool_calls",
		ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)}},
	}}}
	client := &fakeClient{model: llm.ModelInfo{Provider: "fake", Name: "fake-model"}, stream: stream}
	sink := event.NewMemorySink()
	iterator, err := NewIterator(client, sink)
	if err != nil {
		t.Fatalf("create iterator: %v", err)
	}

	result, err := iterator.Run(context.Background(), validIterationInput())
	if err != nil {
		t.Fatalf("run tool iteration: %v", err)
	}
	if result.Kind != IterationToolCalls || result.Candidate != nil || len(result.ToolCalls) != 1 {
		t.Fatalf("unexpected tool iteration result: %#v", result)
	}
	expected := tool.NewCall("call_1", "read", json.RawMessage(`{"path":"README.md"}`))
	if !reflect.DeepEqual(result.ToolCalls[0], expected) {
		t.Fatalf("unexpected normalized call: got %#v, want %#v", result.ToolCalls[0], expected)
	}
	assertEventTypes(t, sink.Snapshot(), []event.Type{event.TypeLLMCallStarted, event.TypeLLMCallCompleted})
}

func TestIteratorRejectsEmptyCandidateAndPublishesError(t *testing.T) {
	stream := &fakeStream{chunks: []llm.StreamChunk{{FinishReason: llm.FinishReasonStop, ProviderFinishReason: "stop"}}}
	client := &fakeClient{model: llm.ModelInfo{Provider: "fake", Name: "fake-model"}, stream: stream}
	sink := event.NewMemorySink()
	iterator, err := NewIterator(client, sink)
	if err != nil {
		t.Fatalf("create iterator: %v", err)
	}

	_, err = iterator.Run(context.Background(), validIterationInput())
	var providerError *llm.ProviderError
	if !errors.As(err, &providerError) || providerError.Kind != llm.ProviderErrorProtocol {
		t.Fatalf("unexpected empty candidate error: %v", err)
	}
	assertEventTypes(t, sink.Snapshot(), []event.Type{event.TypeLLMCallStarted, event.TypeErrorOccurred})
}

func validIterationInput() IterationInput {
	return IterationInput{
		ID:               "iteration_1",
		Messages:         []llm.Message{llm.UserMessage("inspect repository")},
		BaseInstructions: llm.BaseInstructions{Text: "stable Agent protocol"},
		Temperature:      0.2,
		MaxOutputTokens:  512,
	}
}

func assertEventTypes(t *testing.T, events []event.Event, expected []event.Type) {
	t.Helper()
	if len(events) != len(expected) {
		t.Fatalf("unexpected event count: got %d, want %d (%#v)", len(events), len(expected), events)
	}
	for index, expectedType := range expected {
		if events[index].Type() != expectedType {
			t.Fatalf("unexpected event %d: got %q, want %q", index, events[index].Type(), expectedType)
		}
	}
}
