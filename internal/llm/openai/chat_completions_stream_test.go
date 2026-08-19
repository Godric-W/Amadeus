package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	openaisdk "github.com/openai/openai-go/v3"
)

func TestChatCompletionsStreamParsesTextFinishAndUsage(t *testing.T) {
	fixture := strings.Join([]string{
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"test-model","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"test-model","choices":[{"index":0,"delta":{"content":"Hel"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"test-model","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"test-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"test-model","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":2}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	client := chatCompletionsFixtureClient(t, fixture)
	stream, err := openChatCompletionsStream(context.Background(), client, validChatCompletionsDomainRequest())
	if err != nil {
		t.Fatalf("open chat completions stream: %v", err)
	}
	defer stream.Close()

	first, err := stream.Recv()
	if err != nil || first.ID != "chatcmpl_1" || first.ContentDelta != "Hel" {
		t.Fatalf("unexpected first text chunk: %#v, err=%v", first, err)
	}
	second, err := stream.Recv()
	if err != nil || second.ID != "chatcmpl_1" || second.ContentDelta != "lo" {
		t.Fatalf("unexpected second text chunk: %#v, err=%v", second, err)
	}
	completed, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive completion chunk: %v", err)
	}
	if !completed.Completed() || completed.ID != "chatcmpl_1" || completed.FinishReason != llm.FinishReasonStop || completed.ProviderFinishReason != "stop" {
		t.Fatalf("unexpected completion chunk: %#v", completed)
	}
	if completed.Usage == nil || *completed.Usage != (llm.Usage{
		InputTokens:       10,
		CachedInputTokens: 4,
		OutputTokens:      6,
		ReasoningTokens:   2,
		TotalTokens:       16,
	}) {
		t.Fatalf("unexpected completion usage: %#v", completed.Usage)
	}
	_, err = stream.Recv()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected terminal stream error: %v", err)
	}
}

func TestChatCompletionsStreamReturnsFinishWithoutUsage(t *testing.T) {
	fixture := "data: {\"id\":\"chatcmpl_2\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}]}\n\n" +
		"data: [DONE]\n\n"
	client := chatCompletionsFixtureClient(t, fixture)
	stream, err := openChatCompletionsStream(context.Background(), client, validChatCompletionsDomainRequest())
	if err != nil {
		t.Fatalf("open chat completions stream: %v", err)
	}
	defer stream.Close()

	completed, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive completion chunk: %v", err)
	}
	if completed.FinishReason != llm.FinishReasonLength || completed.ProviderFinishReason != "length" || completed.Usage != nil {
		t.Fatalf("unexpected completion without usage: %#v", completed)
	}
}

func TestChatCompletionsStreamAggregatesToolCallFragments(t *testing.T) {
	fixture := strings.Join([]string{
		`data: {"id":"chatcmpl_tools","object":"chat.completion.chunk","created":0,"model":"test-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_","type":"function","function":{"name":"re","arguments":"{\"path\":\""}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl_tools","object":"chat.completion.chunk","created":0,"model":"test-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"1","function":{"name":"ad","arguments":"README.md\"}"}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl_tools","object":"chat.completion.chunk","created":0,"model":"test-model","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		``,
		`data: {"id":"chatcmpl_tools","object":"chat.completion.chunk","created":0,"model":"test-model","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":2,"total_tokens":4}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	stream, err := openChatCompletionsStream(context.Background(), chatCompletionsFixtureClient(t, fixture), validChatCompletionsDomainRequest())
	if err != nil {
		t.Fatalf("open chat stream: %v", err)
	}
	defer stream.Close()

	completed, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive tool completion: %v", err)
	}
	if completed.FinishReason != llm.FinishReasonToolCalls || len(completed.ToolCalls) != 1 {
		t.Fatalf("unexpected tool completion: %#v", completed)
	}
	call := completed.ToolCalls[0]
	if call.ID != "call_1" || call.Name != "read" || string(call.Arguments) != `{"path":"README.md"}` {
		t.Fatalf("unexpected aggregated tool call: %#v", call)
	}
}

func TestChatCompletionsStreamDefersMalformedToolArgumentsToRouter(t *testing.T) {
	fixture := "data: {\"id\":\"chatcmpl_bad\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_bad\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"
	stream, err := openChatCompletionsStream(context.Background(), chatCompletionsFixtureClient(t, fixture), validChatCompletionsDomainRequest())
	if err != nil {
		t.Fatalf("open chat stream: %v", err)
	}
	defer stream.Close()

	completed, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if completed.FinishReason != llm.FinishReasonToolCalls || len(completed.ToolCalls) != 1 || string(completed.ToolCalls[0].Arguments) != `{` {
		t.Fatalf("malformed arguments were not preserved for ToolExecutionService validation: %#v", completed)
	}
}

func TestChatCompletionsDialectNormalizesReasoningContent(t *testing.T) {
	fixture := "data: {\"id\":\"chatcmpl_reasoning\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"think\"},\"finish_reason\":null}]}\n\n"
	for _, name := range []config.ProviderDialect{config.DialectDeepSeek, config.DialectQwen, config.DialectGLM} {
		t.Run(string(name), func(t *testing.T) {
			dialect := mustResolveDialect(t, name)
			stream, err := openChatCompletionsStreamForDialect(context.Background(), chatCompletionsFixtureClient(t, fixture), validChatCompletionsDomainRequest(), dialect)
			if err != nil {
				t.Fatalf("open chat stream: %v", err)
			}
			defer stream.Close()
			chunk, err := stream.Recv()
			if err != nil || chunk.ReasoningDelta != "think" {
				t.Fatalf("unexpected reasoning chunk: %#v, err=%v", chunk, err)
			}
		})
	}
}

func TestChatCompletionsFinishReasonNormalization(t *testing.T) {
	tests := map[string]llm.FinishReason{
		"stop":           llm.FinishReasonStop,
		"length":         llm.FinishReasonLength,
		"tool_calls":     llm.FinishReasonToolCalls,
		"function_call":  llm.FinishReasonToolCalls,
		"content_filter": llm.FinishReasonContentFilter,
		"provider_only":  llm.FinishReasonUnknown,
	}
	for providerReason, expected := range tests {
		if actual := chatCompletionsFinishReason(providerReason); actual != expected {
			t.Fatalf("unexpected finish reason for %q: %q", providerReason, actual)
		}
	}
}

func TestChatCompletionsStreamReturnsDecodeError(t *testing.T) {
	client := chatCompletionsFixtureClient(t, "data: {not-json}\n\n")
	stream, err := openChatCompletionsStream(context.Background(), client, validChatCompletionsDomainRequest())
	if err != nil {
		t.Fatalf("open chat completions stream: %v", err)
	}
	defer stream.Close()

	_, err = stream.Recv()
	var providerError *llm.ProviderError
	if !errors.As(err, &providerError) || providerError.Kind != llm.ProviderErrorProtocol {
		t.Fatalf("unexpected decode error: %v", err)
	}
}

func chatCompletionsFixtureClient(t *testing.T, fixture string) openaisdk.Client {
	t.Helper()
	provider := configuredProvider()
	provider.APIKey = "test-secret"
	provider.BaseURL = "https://chat-stream.example.invalid/v1"
	provider.RequestMaxRetries = 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://chat-stream.example.invalid/v1/chat/completions" {
			t.Fatalf("unexpected streaming request URL: %s", request.URL)
		}
		var requestBody map[string]any
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode streaming request body: %v", err)
		}
		if requestBody["stream"] != true {
			t.Fatalf("streaming request did not enable stream: %#v", requestBody)
		}
		streamOptions, ok := requestBody["stream_options"].(map[string]any)
		if !ok || streamOptions["include_usage"] != true {
			t.Fatalf("streaming request did not enable usage: %#v", requestBody)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(fixture)),
		}, nil
	})}
	client, err := newSDKClient(provider, httpClient)
	if err != nil {
		t.Fatalf("create fixture client: %v", err)
	}
	return client
}
