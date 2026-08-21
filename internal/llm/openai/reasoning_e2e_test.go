package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestAdapterReasoningEffortProviderMockE2E(t *testing.T) {
	tests := []struct {
		name       string
		wireAPI    config.WireAPI
		dialect    config.ProviderDialect
		effort     *llm.ReasoningEffort
		assertBody func(*testing.T, map[string]any)
	}{
		{
			name: "Qwen Responses high", wireAPI: config.WireAPIResponses, dialect: config.DialectQwen,
			effort: effortPointer(llm.ReasoningEffortHigh),
			assertBody: func(t *testing.T, body map[string]any) {
				reasoning := body["reasoning"].(map[string]any)
				if reasoning["effort"] != "high" {
					t.Fatalf("reasoning = %#v", reasoning)
				}
			},
		},
		{
			name: "DeepSeek Chat none", wireAPI: config.WireAPIChatCompletions, dialect: config.DialectDeepSeek,
			effort: effortPointer(llm.ReasoningEffortNone),
			assertBody: func(t *testing.T, body map[string]any) {
				thinking := body["thinking"].(map[string]any)
				if thinking["type"] != "disabled" {
					t.Fatalf("thinking = %#v", thinking)
				}
				if _, ok := body["reasoning_effort"]; ok {
					t.Fatalf("none leaked as reasoning_effort: %#v", body)
				}
			},
		},
		{
			name: "Qwen Chat none", wireAPI: config.WireAPIChatCompletions, dialect: config.DialectQwen,
			effort: effortPointer(llm.ReasoningEffortNone),
			assertBody: func(t *testing.T, body map[string]any) {
				if body["enable_thinking"] != false {
					t.Fatalf("enable_thinking = %#v", body["enable_thinking"])
				}
			},
		},
		{
			name: "GLM Chat max", wireAPI: config.WireAPIChatCompletions, dialect: config.DialectGLM,
			effort: effortPointer(llm.ReasoningEffortMax),
			assertBody: func(t *testing.T, body map[string]any) {
				if body["reasoning_effort"] != "max" {
					t.Fatalf("reasoning_effort = %#v", body["reasoning_effort"])
				}
			},
		},
		{
			name: "Standard Chat unset", wireAPI: config.WireAPIChatCompletions, dialect: config.DialectStandard,
			assertBody: func(t *testing.T, body map[string]any) {
				if _, ok := body["reasoning_effort"]; ok {
					t.Fatalf("unset effort was serialized: %#v", body)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var body map[string]any
			adapter := reasoningFixtureAdapter(t, test.wireAPI, test.dialect, &body)
			request := llm.Request{
				Model:     "test-model",
				Prompt:    llm.Prompt{Input: []llm.ResponseItem{llm.UserMessage("hello")}},
				Reasoning: llm.ReasoningConfigForEffort(test.effort),
			}
			stream, err := adapter.Stream(context.Background(), request)
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			defer stream.Close()
			if _, err := stream.Recv(); err != nil {
				t.Fatalf("receive completion: %v", err)
			}
			test.assertBody(t, body)
		})
	}
}

func reasoningFixtureAdapter(t *testing.T, wireAPI config.WireAPI, dialectName config.ProviderDialect, body *map[string]any) *Adapter {
	t.Helper()
	provider := configuredProvider()
	provider.WireAPI = wireAPI
	provider.Dialect = dialectName
	provider.APIKey = "test-secret"
	provider.BaseURL = "https://reasoning.example.invalid/v1"
	provider.RequestMaxRetries = 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		fixture := "data: {\"id\":\"chatcmpl_effort\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
		if wireAPI == config.WireAPIResponses {
			fixture = strings.Join([]string{
				`event: response.completed`,
				`data: {"type":"response.completed","sequence_number":0,"response":{"id":"resp_effort","status":"completed","usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}}`,
				``, `data: [DONE]`, ``,
			}, "\n")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(fixture)),
		}, nil
	})}
	sdk, err := newSDKClient(provider, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	dialect, err := resolveDialect(dialectName)
	if err != nil {
		t.Fatal(err)
	}
	return &Adapter{sdk: sdk, providerName: "provider", model: "test-model", provider: provider, dialect: dialect}
}
