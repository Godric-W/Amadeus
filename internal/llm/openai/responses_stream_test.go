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

func TestResponsesStreamNormalizesTextReasoningCompletionAndUsage(t *testing.T) {
	fixture := strings.Join([]string{
		`sse: response fixture`,
		`event: response.created`,
		`data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_1","status":"in_progress"}}`,
		``,
		`event: response.reasoning_summary_text.delta`,
		`data: {"type":"response.reasoning_summary_text.delta","sequence_number":1,"item_id":"reasoning_1","output_index":0,"summary_index":0,"delta":"think"}`,
		``,
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","sequence_number":2,"item_id":"message_1","output_index":1,"content_index":0,"delta":"hel","logprobs":[]}`,
		``,
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","sequence_number":3,"item_id":"message_1","output_index":1,"content_index":0,"delta":"lo","logprobs":[]}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed","sequence_number":4,"response":{"id":"resp_1","status":"completed","usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":4,"cache_write_tokens":0},"output_tokens":6,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":16}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	client := responsesFixtureClient(t, fixture)

	stream, err := openResponsesStream(context.Background(), client, validResponsesDomainRequest())
	if err != nil {
		t.Fatalf("open responses stream: %v", err)
	}
	defer stream.Close()

	reasoning, err := stream.Recv()
	if err != nil || reasoning.ID != "resp_1" || reasoning.ReasoningDelta != "think" {
		t.Fatalf("unexpected reasoning chunk: %#v, err=%v", reasoning, err)
	}
	first, err := stream.Recv()
	if err != nil || first.ID != "resp_1" || first.ContentDelta != "hel" {
		t.Fatalf("unexpected first text chunk: %#v, err=%v", first, err)
	}
	second, err := stream.Recv()
	if err != nil || second.ContentDelta != "lo" {
		t.Fatalf("unexpected second text chunk: %#v, err=%v", second, err)
	}
	completed, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive completion chunk: %v", err)
	}
	if !completed.Completed() || completed.FinishReason != llm.FinishReasonStop || completed.ProviderFinishReason != "completed" {
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

func TestResponsesStreamNormalizesIncompleteReason(t *testing.T) {
	fixture := "event: response.incomplete\n" +
		"data: {\"type\":\"response.incomplete\",\"sequence_number\":1,\"response\":{\"id\":\"resp_2\",\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"max_output_tokens\"},\"usage\":{\"input_tokens\":3,\"input_tokens_details\":{\"cached_tokens\":0,\"cache_write_tokens\":0},\"output_tokens\":5,\"output_tokens_details\":{\"reasoning_tokens\":0},\"total_tokens\":8}}}\n\n"
	client := responsesFixtureClient(t, fixture)
	stream, err := openResponsesStream(context.Background(), client, validResponsesDomainRequest())
	if err != nil {
		t.Fatalf("open responses stream: %v", err)
	}
	defer stream.Close()

	chunk, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive incomplete chunk: %v", err)
	}
	if chunk.FinishReason != llm.FinishReasonLength || chunk.ProviderFinishReason != "max_output_tokens" {
		t.Fatalf("unexpected incomplete chunk: %#v", chunk)
	}
}

func TestResponsesStreamReturnsProviderErrorEvent(t *testing.T) {
	fixture := "event: error\n" +
		"data: {\"type\":\"error\",\"sequence_number\":1,\"code\":\"server_error\",\"message\":\"temporary failure\",\"param\":\"model\"}\n\n"
	client := responsesFixtureClient(t, fixture)
	stream, err := openResponsesStream(context.Background(), client, validResponsesDomainRequest())
	if err != nil {
		t.Fatalf("open responses stream: %v", err)
	}
	defer stream.Close()

	_, err = stream.Recv()
	var providerError *llm.ProviderError
	if !errors.As(err, &providerError) {
		t.Fatalf("unexpected stream error: %v", err)
	}
	if providerError.Kind != llm.ProviderErrorUnavailable || providerError.Code != "server_error" || providerError.Param != "model" || providerError.Message != "temporary failure" {
		t.Fatalf("unexpected provider stream error: %#v", providerError)
	}
}

func TestResponsesStreamReturnsFailedResponse(t *testing.T) {
	fixture := "event: response.failed\n" +
		"data: {\"type\":\"response.failed\",\"sequence_number\":1,\"response\":{\"id\":\"resp_failed\",\"status\":\"failed\",\"error\":{\"code\":\"rate_limit_exceeded\",\"message\":\"slow down\"}}}\n\n"
	client := responsesFixtureClient(t, fixture)
	stream, err := openResponsesStream(context.Background(), client, validResponsesDomainRequest())
	if err != nil {
		t.Fatalf("open responses stream: %v", err)
	}
	defer stream.Close()

	_, err = stream.Recv()
	var providerError *llm.ProviderError
	if !errors.As(err, &providerError) {
		t.Fatalf("unexpected failed response error: %v", err)
	}
	if providerError.Kind != llm.ProviderErrorRateLimit || providerError.Code != "rate_limit_exceeded" || providerError.Message != "slow down" || providerError.RequestID != "resp_failed" {
		t.Fatalf("unexpected failed response details: %#v", providerError)
	}
}

func TestResponsesStreamReturnsDecodeError(t *testing.T) {
	client := responsesFixtureClient(t, "event: response.output_text.delta\ndata: {not-json}\n\n")
	stream, err := openResponsesStream(context.Background(), client, validResponsesDomainRequest())
	if err != nil {
		t.Fatalf("open responses stream: %v", err)
	}
	defer stream.Close()

	_, err = stream.Recv()
	var providerError *llm.ProviderError
	if !errors.As(err, &providerError) || providerError.Kind != llm.ProviderErrorProtocol {
		t.Fatalf("unexpected decode error: %v", err)
	}
}

func responsesFixtureClient(t *testing.T, fixture string) openaisdk.Client {
	t.Helper()
	provider := config.Default().Providers[config.DefaultProviderName]
	provider.APIKey = "test-secret"
	provider.BaseURL = "https://stream.example.invalid/v1"
	provider.MaxRetries = 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://stream.example.invalid/v1/responses" {
			t.Fatalf("unexpected streaming request URL: %s", request.URL)
		}
		var requestBody map[string]any
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode streaming request body: %v", err)
		}
		if requestBody["stream"] != true {
			t.Fatalf("streaming request did not enable stream: %#v", requestBody)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(fixture)),
		}, nil
	})}
	client, err := newClient(provider, httpClient)
	if err != nil {
		t.Fatalf("create fixture client: %v", err)
	}
	return client
}
