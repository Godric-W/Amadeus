package engine

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type scriptedModelClient struct {
	mu       sync.Mutex
	streams  []llm.Stream
	requests []llm.Request
}

func (client *scriptedModelClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unexpected Complete call")
}

func (client *scriptedModelClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.requests = append(client.requests, request)
	if len(client.streams) == 0 {
		return nil, errors.New("no scripted stream")
	}
	stream := client.streams[0]
	client.streams = client.streams[1:]
	return stream, nil
}

func (*scriptedModelClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "test-provider", Name: "test-model", MaxOutputTokens: 1024}
}

func (*scriptedModelClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

type scriptedStreamResult struct {
	chunk llm.StreamChunk
	err   error
}

type scriptedModelStream struct {
	results  []scriptedStreamResult
	closeErr error
}

func (stream *scriptedModelStream) Recv() (llm.StreamChunk, error) {
	if len(stream.results) == 0 {
		return llm.StreamChunk{}, io.EOF
	}
	result := stream.results[0]
	stream.results = stream.results[1:]
	return result.chunk, result.err
}

func (stream *scriptedModelStream) Close() error { return stream.closeErr }

func TestModelClientSessionRetriesDroppedStreamAndReplacesAttemptDraft(t *testing.T) {
	client := &scriptedModelClient{streams: []llm.Stream{
		&scriptedModelStream{results: []scriptedStreamResult{
			{chunk: llm.StreamChunk{ContentDelta: "partial", ReasoningDelta: "old reasoning"}},
			{err: retryableNetworkError("connection reset")},
		}},
		&scriptedModelStream{results: []scriptedStreamResult{
			{chunk: llm.StreamChunk{ContentDelta: "recovered", ReasoningDelta: "new reasoning"}},
			{chunk: llm.StreamChunk{FinishReason: llm.FinishReasonStop}},
		}},
	}}
	sink := protocol.NewMemorySink()
	session := newTestModelClientSession(t, client, 2, time.Second)

	result, err := session.Sample(context.Background(), sampleRequest(sink))
	if err != nil {
		t.Fatalf("sample after reconnect: %v", err)
	}
	if result.Response.Message.Content != "recovered" {
		t.Fatalf("retry response reused partial attempt content: %q", result.Response.Message.Content)
	}
	if len(client.requests) != 2 {
		t.Fatalf("unexpected stream attempts: %d", len(client.requests))
	}

	events := sink.Snapshot()
	var retryEvents int
	var assistantResetEvents int
	var reasoningResetEvents int
	var terminalErrors int
	state := protocol.NewTranscriptState("memory-thread")
	for _, event := range events {
		if streamError, ok := event.Message.(protocol.StreamError); ok {
			if streamError.WillRetry {
				retryEvents++
				if streamError.Message != "Reconnecting... 1/2" {
					t.Fatalf("unexpected retry message: %#v", streamError)
				}
			} else {
				terminalErrors++
			}
		}
		if delta, ok := event.Message.(protocol.AssistantMessageDelta); ok && delta.Reset {
			assistantResetEvents++
		}
		if delta, ok := event.Message.(protocol.ReasoningDelta); ok && delta.Reset {
			reasoningResetEvents++
		}
		if err := state.Apply(event); err != nil {
			t.Fatalf("apply retry event: %v", err)
		}
	}
	if retryEvents != 1 || assistantResetEvents != 1 || reasoningResetEvents != 1 || terminalErrors != 0 {
		t.Fatalf("unexpected retry lifecycle: retry=%d assistant_reset=%d reasoning_reset=%d terminal=%d events=%#v", retryEvents, assistantResetEvents, reasoningResetEvents, terminalErrors, events)
	}
	active := state.Active["sample-1:assistant"]
	if active.Text != "recovered" {
		t.Fatalf("transcript draft was not replaced: %#v", active)
	}
	if reasoning := state.Active["sample-1:reasoning"]; reasoning.Text != "new reasoning" {
		t.Fatalf("reasoning draft was not replaced: %#v", reasoning)
	}
}

func TestModelClientSessionDiscardsFailedAttemptToolCalls(t *testing.T) {
	client := &scriptedModelClient{streams: []llm.Stream{
		&scriptedModelStream{results: []scriptedStreamResult{
			{chunk: llm.StreamChunk{ToolCalls: []llm.ToolCall{{ID: "stale", Name: "read", Arguments: []byte(`{"path":"old"}`)}}}},
			{err: retryableNetworkError("connection reset")},
		}},
		&scriptedModelStream{results: []scriptedStreamResult{
			{chunk: llm.StreamChunk{ContentDelta: "recovered"}},
			{chunk: llm.StreamChunk{FinishReason: llm.FinishReasonStop}},
		}},
	}}
	session := newTestModelClientSession(t, client, 1, time.Second)
	result, err := session.Sample(context.Background(), sampleRequest(protocol.NewMemorySink()))
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != SampleFinal || len(result.ToolCalls) != 0 || len(result.Response.Message.ToolCalls) != 0 {
		t.Fatalf("failed attempt tool calls leaked: %#v", result)
	}
}

func TestModelClientSessionPublishesTerminalErrorAfterRetryExhaustion(t *testing.T) {
	client := &scriptedModelClient{streams: []llm.Stream{
		failingStream("first"), failingStream("second"), failingStream("third"),
	}}
	sink := protocol.NewMemorySink()
	session := newTestModelClientSession(t, client, 2, time.Second)

	_, err := session.Sample(context.Background(), sampleRequest(sink))
	if err == nil {
		t.Fatal("expected retry exhaustion error")
	}
	var retryEvents int
	var terminalEvents int
	for _, event := range sink.Snapshot() {
		if streamError, ok := event.Message.(protocol.StreamError); ok {
			if streamError.WillRetry {
				retryEvents++
			} else {
				terminalEvents++
			}
		}
	}
	if retryEvents != 2 || terminalEvents != 1 || len(client.requests) != 3 {
		t.Fatalf("unexpected exhausted lifecycle: retries=%d terminal=%d requests=%d", retryEvents, terminalEvents, len(client.requests))
	}
}

func TestModelClientSessionHonorsProviderRetryDelay(t *testing.T) {
	client := &scriptedModelClient{streams: []llm.Stream{
		&scriptedModelStream{results: []scriptedStreamResult{{err: &llm.ProviderError{
			Kind: llm.ProviderErrorRateLimit, Message: "slow down", Retryable: true, RetryDelay: 125 * time.Millisecond,
		}}}},
		&scriptedModelStream{results: []scriptedStreamResult{{chunk: llm.StreamChunk{ContentDelta: "ok"}}, {chunk: llm.StreamChunk{FinishReason: llm.FinishReasonStop}}}},
	}}
	sink := protocol.NewMemorySink()
	session := newTestModelClientSession(t, client, 1, time.Second)
	var delays []time.Duration
	session.retry.sleep = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}

	if _, err := session.Sample(context.Background(), sampleRequest(sink)); err != nil {
		t.Fatalf("sample with Retry-After: %v", err)
	}
	if len(delays) != 1 || delays[0] != 125*time.Millisecond {
		t.Fatalf("unexpected retry delays: %v", delays)
	}
	for _, event := range sink.Snapshot() {
		streamError, ok := event.Message.(protocol.StreamError)
		if !ok || !streamError.WillRetry {
			continue
		}
		if streamError.ProviderError == nil || !streamError.ProviderError.Retryable || streamError.ProviderError.RetryDelay != 125*time.Millisecond {
			t.Fatalf("retry provider info = %#v", streamError.ProviderError)
		}
		return
	}
	t.Fatal("missing retry stream event")
}

func TestModelClientSessionCancellationStopsBackoffWithoutTerminalError(t *testing.T) {
	client := &scriptedModelClient{streams: []llm.Stream{failingStream("disconnect")}}
	sink := protocol.NewMemorySink()
	session := newTestModelClientSession(t, client, 3, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	session.retry.sleep = func(context.Context, time.Duration) error {
		cancel()
		return ctx.Err()
	}

	_, err := session.Sample(ctx, sampleRequest(sink))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation error: %v", err)
	}
	var retryEvents int
	var terminalEvents int
	for _, event := range sink.Snapshot() {
		if streamError, ok := event.Message.(protocol.StreamError); ok {
			if streamError.WillRetry {
				retryEvents++
			} else {
				terminalEvents++
			}
		}
	}
	if retryEvents != 1 || terminalEvents != 0 {
		t.Fatalf("cancellation emitted wrong errors: retry=%d terminal=%d", retryEvents, terminalEvents)
	}
}

func TestModelClientSessionDoesNotRetryCloseFailureAfterCompletedResponse(t *testing.T) {
	client := &scriptedModelClient{streams: []llm.Stream{&scriptedModelStream{
		results:  []scriptedStreamResult{{chunk: llm.StreamChunk{ContentDelta: "done"}}, {chunk: llm.StreamChunk{FinishReason: llm.FinishReasonStop}}},
		closeErr: retryableNetworkError("close failed"),
	}}}
	sink := protocol.NewMemorySink()
	session := newTestModelClientSession(t, client, 2, time.Second)

	result, err := session.Sample(context.Background(), sampleRequest(sink))
	if err != nil || result.Response.Message.Content != "done" {
		t.Fatalf("completed response should win over close error: result=%#v err=%v", result, err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("completed response was retried: %d", len(client.requests))
	}
}

func TestModelClientSessionRetriesIdleTimeout(t *testing.T) {
	blocking := newBlockingModelStream()
	client := &scriptedModelClient{streams: []llm.Stream{
		blocking,
		&scriptedModelStream{results: []scriptedStreamResult{{chunk: llm.StreamChunk{ContentDelta: "after idle"}}, {chunk: llm.StreamChunk{FinishReason: llm.FinishReasonStop}}}},
	}}
	sink := protocol.NewMemorySink()
	session := newTestModelClientSession(t, client, 1, 5*time.Millisecond)

	result, err := session.Sample(context.Background(), sampleRequest(sink))
	if err != nil || result.Response.Message.Content != "after idle" {
		t.Fatalf("idle reconnect failed: result=%#v err=%v", result, err)
	}
	if !blocking.closed {
		t.Fatal("idle stream was not closed")
	}
}

type blockingModelStream struct {
	done   chan struct{}
	once   sync.Once
	closed bool
}

func newBlockingModelStream() *blockingModelStream {
	return &blockingModelStream{done: make(chan struct{})}
}

func (stream *blockingModelStream) Recv() (llm.StreamChunk, error) {
	<-stream.done
	return llm.StreamChunk{}, context.Canceled
}

func (stream *blockingModelStream) Close() error {
	stream.once.Do(func() {
		stream.closed = true
		close(stream.done)
	})
	return nil
}

func newTestModelClientSession(t *testing.T, client llm.Client, maxRetries int, idleTimeout time.Duration) *ModelClientSession {
	t.Helper()
	session, err := NewModelClientSession(client, ModelClientSessionConfig{
		StreamMaxRetries: maxRetries, StreamIdleTimeout: idleTimeout,
		backoff: func(int) time.Duration { return 0 },
		sleep:   func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatalf("new model client session: %v", err)
	}
	return session
}

func sampleRequest(events protocol.EventSink) SampleRequest {
	return SampleRequest{
		ID: "sample-1", Messages: []llm.ResponseItem{llm.UserMessage("hello")},
		BaseInstructions: llm.BaseInstructions{Text: "help"}, MaxOutputTokens: 128, Events: events,
	}
}

func failingStream(message string) llm.Stream {
	return &scriptedModelStream{results: []scriptedStreamResult{{err: retryableNetworkError(message)}}}
}

func retryableNetworkError(message string) error {
	return &llm.ProviderError{
		Kind: llm.ProviderErrorNetwork, Message: message, AdditionalDetails: message, Retryable: true,
	}
}

func TestRetryErrorDetailsAreSafeAndStructured(t *testing.T) {
	client := &scriptedModelClient{streams: []llm.Stream{failingStream("network unavailable")}}
	sink := protocol.NewMemorySink()
	session := newTestModelClientSession(t, client, 0, time.Second)

	_, _ = session.Sample(context.Background(), sampleRequest(sink))
	events := sink.Snapshot()
	if len(events) == 0 {
		t.Fatal("missing terminal stream error")
	}
	streamError, ok := events[len(events)-1].Message.(protocol.StreamError)
	if !ok || streamError.WillRetry || streamError.ProviderError == nil || streamError.ProviderError.Kind != string(llm.ProviderErrorNetwork) {
		t.Fatalf("unexpected structured stream error: %#v", events[len(events)-1].Message)
	}
	if streamError.AdditionalDetails == nil || !strings.Contains(*streamError.AdditionalDetails, "network unavailable") {
		t.Fatalf("missing stream error details: %#v", streamError)
	}
}
