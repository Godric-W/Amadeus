package runtime

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type fakeClient struct {
	model        llm.ModelInfo
	stream       llm.Stream
	streams      []llm.Stream
	streamError  error
	request      llm.Request
	requests     []llm.Request
	streamCalled bool
}

func (client *fakeClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("complete is not used by streaming session")
}

func (client *fakeClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.streamCalled = true
	client.request = request
	client.requests = append(client.requests, request)
	if len(client.streams) > 0 {
		stream := client.streams[0]
		client.streams = client.streams[1:]
		return stream, client.streamError
	}
	return client.stream, client.streamError
}

func (client *fakeClient) Model() llm.ModelInfo {
	return client.model
}

func (client *fakeClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

type fakeStream struct {
	chunks   []llm.StreamChunk
	recvErr  error
	next     int
	closed   bool
	closeErr error
}

func (stream *fakeStream) Recv() (llm.StreamChunk, error) {
	if stream.next < len(stream.chunks) {
		chunk := stream.chunks[stream.next]
		stream.next++
		return chunk, nil
	}
	if stream.recvErr != nil {
		return llm.StreamChunk{}, stream.recvErr
	}
	return llm.StreamChunk{}, io.EOF
}

func (stream *fakeStream) Close() error {
	stream.closed = true
	return stream.closeErr
}

func TestSessionRunsSingleStreamingTurn(t *testing.T) {
	usage := llm.Usage{InputTokens: 3, CachedInputTokens: 1, OutputTokens: 4, ReasoningTokens: 2, TotalTokens: 7}
	stream := &fakeStream{chunks: []llm.StreamChunk{
		{ID: "response_1", ReasoningDelta: "think "},
		{ID: "response_1", ContentDelta: "hel"},
		{ID: "response_1", ContentDelta: "lo"},
		{ID: "response_1", RequestID: "request_1", FinishReason: llm.FinishReasonStop, ProviderFinishReason: "stop", Usage: &usage},
	}}
	client := &fakeClient{
		model:  llm.ModelInfo{Provider: "fake", Name: "fake-model"},
		stream: stream,
	}
	sink := event.NewMemorySink()
	session, err := NewSession(client, sink, SessionOptions{Temperature: 0.4, MaxOutputTokens: 1024})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	response, err := session.RunTurn(context.Background(), TurnInput{ID: "turn_1", Content: "hi"})
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	if !client.streamCalled || client.request.Model != "fake-model" || client.request.Temperature != 0.4 || client.request.MaxOutputTokens != 1024 {
		t.Fatalf("unexpected LLM request: %#v", client.request)
	}
	if len(client.request.Messages) != 1 || !reflect.DeepEqual(client.request.Messages[0], llm.UserMessage("hi")) {
		t.Fatalf("unexpected request messages: %#v", client.request.Messages)
	}
	if response.ID != "response_1" || response.RequestID != "request_1" || !reflect.DeepEqual(response.Message, llm.Message{Role: llm.RoleAssistant, Content: "hello", Reasoning: "think "}) {
		t.Fatalf("unexpected aggregated response: %#v", response)
	}
	if response.FinishReason != llm.FinishReasonStop || response.ProviderFinishReason != "stop" || response.Usage != usage {
		t.Fatalf("unexpected response metadata: %#v", response)
	}
	if !stream.closed {
		t.Fatal("session did not close stream")
	}

	events := sink.Snapshot()
	expectedTypes := []event.Type{
		event.TypeTurnStarted,
		event.TypeReasoningDelta,
		event.TypeTextDelta,
		event.TypeTextDelta,
		event.TypeUsageUpdated,
		event.TypeTurnCompleted,
	}
	if len(events) != len(expectedTypes) {
		t.Fatalf("unexpected event count: got %d, want %d", len(events), len(expectedTypes))
	}
	for index, expectedType := range expectedTypes {
		if events[index].Type() != expectedType {
			t.Fatalf("unexpected event at %d: got %q, want %q", index, events[index].Type(), expectedType)
		}
	}
	completed := events[len(events)-1].(event.TurnCompleted)
	if completed.TurnID != "turn_1" || completed.ResponseID != "response_1" || completed.RequestID != "request_1" || completed.FinishReason != llm.FinishReasonStop {
		t.Fatalf("unexpected completion event: %#v", completed)
	}
}

func TestSessionPublishesProviderError(t *testing.T) {
	providerError := &llm.ProviderError{
		Kind:       llm.ProviderErrorRateLimit,
		StatusCode: 429,
		Code:       "rate_limit_exceeded",
		RequestID:  "request_error",
		Message:    "slow down",
	}
	client := &fakeClient{
		model:       llm.ModelInfo{Provider: "fake", Name: "fake-model"},
		streamError: providerError,
	}
	sink := event.NewMemorySink()
	session, err := NewSession(client, sink, SessionOptions{MaxOutputTokens: 128})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err = session.RunTurn(context.Background(), TurnInput{ID: "turn_error", Content: "hi"})
	if !errors.Is(err, providerError) {
		t.Fatalf("unexpected turn error: %v", err)
	}
	events := sink.Snapshot()
	if len(events) != 2 || events[0].Type() != event.TypeTurnStarted || events[1].Type() != event.TypeErrorOccurred {
		t.Fatalf("unexpected failure events: %#v", events)
	}
	errorEvent := events[1].(event.ErrorOccurred)
	if errorEvent.Error.Kind != llm.ProviderErrorRateLimit || errorEvent.Error.Code != "rate_limit_exceeded" || errorEvent.Error.RequestID != "request_error" {
		t.Fatalf("unexpected error event: %#v", errorEvent)
	}
}

func TestSessionIncludesSuccessfulTurnsInConversationHistory(t *testing.T) {
	client := &fakeClient{
		model: llm.ModelInfo{Provider: "fake", Name: "fake-model"},
		streams: []llm.Stream{
			&fakeStream{chunks: []llm.StreamChunk{
				{ID: "response_1", ContentDelta: "first answer"},
				{ID: "response_1", FinishReason: llm.FinishReasonStop},
			}},
			&fakeStream{chunks: []llm.StreamChunk{
				{ID: "response_2", ContentDelta: "second answer"},
				{ID: "response_2", FinishReason: llm.FinishReasonStop},
			}},
		},
	}
	session, err := NewSession(client, event.NewMemorySink(), SessionOptions{MaxOutputTokens: 128})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := session.RunTurn(context.Background(), TurnInput{ID: "turn_1", Content: "first question"}); err != nil {
		t.Fatalf("run first turn: %v", err)
	}
	if _, err := session.RunTurn(context.Background(), TurnInput{ID: "turn_2", Content: "second question"}); err != nil {
		t.Fatalf("run second turn: %v", err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("unexpected request count: %#v", client.requests)
	}
	expected := []llm.Message{
		llm.UserMessage("first question"),
		llm.AssistantMessage("first answer"),
		llm.UserMessage("second question"),
	}
	if len(client.requests[1].Messages) != len(expected) {
		t.Fatalf("unexpected second request history: %#v", client.requests[1].Messages)
	}
	for index, message := range expected {
		if !reflect.DeepEqual(client.requests[1].Messages[index], message) {
			t.Fatalf("unexpected history message %d: got %#v, want %#v", index, client.requests[1].Messages[index], message)
		}
	}
}

func TestSessionTreatsEOFBeforeCompletionAsProtocolError(t *testing.T) {
	stream := &fakeStream{chunks: []llm.StreamChunk{{ID: "response_2", ContentDelta: "partial"}}}
	client := &fakeClient{
		model:  llm.ModelInfo{Provider: "fake", Name: "fake-model"},
		stream: stream,
	}
	sink := event.NewMemorySink()
	session, err := NewSession(client, sink, SessionOptions{MaxOutputTokens: 128})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	response, err := session.RunTurn(context.Background(), TurnInput{ID: "turn_partial", Content: "hi"})
	var providerError *llm.ProviderError
	if !errors.As(err, &providerError) || providerError.Kind != llm.ProviderErrorProtocol {
		t.Fatalf("unexpected premature EOF error: %v", err)
	}
	if response.Message.Content != "partial" || !stream.closed {
		t.Fatalf("unexpected partial response state: %#v, closed=%v", response, stream.closed)
	}
	events := sink.Snapshot()
	if len(events) != 3 || events[0].Type() != event.TypeTurnStarted || events[1].Type() != event.TypeTextDelta || events[2].Type() != event.TypeErrorOccurred {
		t.Fatalf("unexpected premature EOF events: %#v", events)
	}
}

func TestSessionValidatesConfigurationAndInput(t *testing.T) {
	client := &fakeClient{model: llm.ModelInfo{Provider: "fake", Name: "fake-model"}}
	sink := event.NewMemorySink()
	tests := []struct {
		name    string
		client  llm.Client
		sink    event.Sink
		options SessionOptions
	}{
		{name: "nil client", sink: sink, options: SessionOptions{MaxOutputTokens: 1}},
		{name: "nil sink", client: client, options: SessionOptions{MaxOutputTokens: 1}},
		{name: "empty model", client: &fakeClient{}, sink: sink, options: SessionOptions{MaxOutputTokens: 1}},
		{name: "invalid temperature", client: client, sink: sink, options: SessionOptions{Temperature: 2.1, MaxOutputTokens: 1}},
		{name: "invalid max tokens", client: client, sink: sink},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSession(test.client, test.sink, test.options); err == nil {
				t.Fatal("expected session configuration error")
			}
		})
	}

	session, err := NewSession(client, sink, SessionOptions{MaxOutputTokens: 1})
	if err != nil {
		t.Fatalf("create valid session: %v", err)
	}
	if _, err := session.RunTurn(context.Background(), TurnInput{Content: "hi"}); err == nil {
		t.Fatal("expected empty turn ID error")
	}
	if _, err := session.RunTurn(context.Background(), TurnInput{ID: "turn_1", Content: "  "}); err == nil {
		t.Fatal("expected empty turn content error")
	}
	if client.streamCalled || sink.Len() != 0 {
		t.Fatal("invalid turn reached LLM or event sink")
	}
}

var _ llm.Client = (*fakeClient)(nil)
var _ llm.Stream = (*fakeStream)(nil)
