package react

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
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
	rootSink := protocol.NewMemorySink()
	sink, err := protocol.NewScopedSink(rootSink, "thread-1", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
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
	events := rootSink.Snapshot()
	if len(events) != 8 {
		t.Fatalf("unexpected event count: got %d (%#v)", len(events), events)
	}
	if _, ok := events[0].Message.(protocol.ItemStarted); !ok {
		t.Fatalf("event 0 is not ItemStarted: %#v", events[0])
	}
	if _, ok := events[1].Message.(protocol.ReasoningDelta); !ok {
		t.Fatalf("event 1 is not ReasoningDelta: %#v", events[1])
	}
	if _, ok := events[2].Message.(protocol.ItemStarted); !ok {
		t.Fatalf("event 2 is not ItemStarted: %#v", events[2])
	}
	if _, ok := events[3].Message.(protocol.AssistantMessageDelta); !ok {
		t.Fatalf("event 3 is not AssistantMessageDelta: %#v", events[3])
	}
	if _, ok := events[4].Message.(protocol.AssistantMessageDelta); !ok {
		t.Fatalf("event 4 is not AssistantMessageDelta: %#v", events[4])
	}
	if _, ok := events[5].Message.(protocol.ThreadTokenUsageUpdated); !ok {
		t.Fatalf("event 5 is not ThreadTokenUsageUpdated: %#v", events[5])
	}
	if _, ok := events[6].Message.(protocol.ItemCompleted); !ok {
		t.Fatalf("event 6 is not assistant ItemCompleted: %#v", events[6])
	}
	if _, ok := events[7].Message.(protocol.ItemCompleted); !ok {
		t.Fatalf("event 7 is not reasoning ItemCompleted: %#v", events[7])
	}
}

func TestIteratorPreservesAssembledSystemPrompt(t *testing.T) {
	stream := &fakeStream{chunks: []llm.StreamChunk{{ContentDelta: "done", FinishReason: llm.FinishReasonStop}}}
	client := &fakeClient{model: llm.ModelInfo{Provider: "fake", Name: "fake-model"}, stream: stream}
	rootSink := protocol.NewMemorySink()
	sink, err := protocol.NewScopedSink(rootSink, "thread-1", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := NewIterator(client, sink)
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
	rootSink := protocol.NewMemorySink()
	sink, err := protocol.NewScopedSink(rootSink, "thread-1", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := NewIterator(client, sink)
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
	rootSink := protocol.NewMemorySink()
	sink, err := protocol.NewScopedSink(rootSink, "thread-1", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := NewIterator(client, sink)
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
	rootSink := protocol.NewMemorySink()
	sink, err := protocol.NewScopedSink(rootSink, "thread-1", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
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
	if len(rootSink.Snapshot()) != 0 {
		t.Fatalf("tool-call-only iteration should not publish text lifecycle events: %#v", rootSink.Snapshot())
	}
}

func TestIteratorRejectsEmptyCandidateAndPublishesError(t *testing.T) {
	stream := &fakeStream{chunks: []llm.StreamChunk{{FinishReason: llm.FinishReasonStop, ProviderFinishReason: "stop"}}}
	client := &fakeClient{model: llm.ModelInfo{Provider: "fake", Name: "fake-model"}, stream: stream}
	rootSink := protocol.NewMemorySink()
	sink, err := protocol.NewScopedSink(rootSink, "thread-1", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := NewIterator(client, sink)
	if err != nil {
		t.Fatalf("create iterator: %v", err)
	}

	_, err = iterator.Run(context.Background(), validIterationInput())
	var providerError *llm.ProviderError
	if !errors.As(err, &providerError) || providerError.Kind != llm.ProviderErrorProtocol {
		t.Fatalf("unexpected empty candidate error: %v", err)
	}
	events := rootSink.Snapshot()
	if len(events) != 1 {
		t.Fatalf("expected one stream error event: %#v", events)
	}
	if message, ok := events[0].Message.(protocol.StreamError); !ok || message.Error == "" {
		t.Fatalf("expected StreamError event: %#v", events[0])
	}
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
