package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
)

type mockProviderRequest struct {
	path          string
	authorization string
	body          map[string]any
}

func TestChatCommandProviderMockIntegration(t *testing.T) {
	tests := []struct {
		name         string
		api          config.APIMode
		expectedPath string
		fixture      string
	}{
		{
			name:         "responses",
			api:          config.APIResponses,
			expectedPath: "/v1/responses",
			fixture: strings.Join([]string{
				`event: response.created`,
				`data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_mock","status":"in_progress"}}`,
				``,
				`event: response.output_text.delta`,
				`data: {"type":"response.output_text.delta","sequence_number":1,"item_id":"message_1","output_index":0,"content_index":0,"delta":"hello","logprobs":[]}`,
				``,
				`event: response.completed`,
				`data: {"type":"response.completed","sequence_number":2,"response":{"id":"resp_mock","status":"completed","usage":{"input_tokens":3,"input_tokens_details":{"cached_tokens":1,"cache_write_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":5}}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"),
		},
		{
			name:         "chat completions",
			api:          config.APIChatCompletions,
			expectedPath: "/v1/chat/completions",
			fixture: strings.Join([]string{
				`data: {"id":"chatcmpl_mock","object":"chat.completion.chunk","created":0,"model":"mock-model","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
				``,
				`data: {"id":"chatcmpl_mock","object":"chat.completion.chunk","created":0,"model":"mock-model","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
				``,
				`data: {"id":"chatcmpl_mock","object":"chat.completion.chunk","created":0,"model":"mock-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
				``,
				`data: {"id":"chatcmpl_mock","object":"chat.completion.chunk","created":0,"model":"mock-model","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5,"prompt_tokens_details":{"cached_tokens":1},"completion_tokens_details":{"reasoning_tokens":0}}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestReceived := make(chan mockProviderRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Errorf("decode mock provider request: %v", err)
				}
				requestReceived <- mockProviderRequest{
					path:          request.URL.Path,
					authorization: request.Header.Get("Authorization"),
					body:          body,
				}
				writer.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(writer, test.fixture)
			}))
			defer server.Close()

			amadeusRoot := t.TempDir()
			writeCommandConfig(t, filepath.Join(amadeusRoot, "config.yaml"), fmt.Sprintf(`
providers:
  mock:
    api: %s
    api_key: test-secret
    base_url: %s/v1
    model: mock-model
    max_retries: 0
default_provider: mock
`, test.api, server.URL))
			command := newRootCommandWithRuntime(&configFlags{}, commandRuntime{
				amadeusRoot: amadeusRoot,
				lookupEnv:   emptyEnvLookup,
				turnContextFactory: func(parent context.Context) (context.Context, context.CancelFunc) {
					return context.WithCancel(parent)
				},
			})
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			command.SetIn(strings.NewReader("hi\n/exit\n"))
			command.SetOut(&stdout)
			command.SetErr(&stderr)
			command.SetArgs([]string{"chat"})

			if err := command.Execute(); err != nil {
				t.Fatalf("execute mock provider chat: %v", err)
			}
			if stdout.String() != "hello\n" || stderr.Len() != 0 {
				t.Fatalf("unexpected command output: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), "test-secret") {
				t.Fatal("command output leaked mock API key")
			}

			received := <-requestReceived
			if received.path != test.expectedPath {
				t.Fatalf("unexpected provider path: got %q, want %q", received.path, test.expectedPath)
			}
			if received.authorization != "Bearer test-secret" {
				t.Fatalf("unexpected authorization header: %q", received.authorization)
			}
			assertMockProviderRequest(t, test.api, received.body)
		})
	}
}

func assertMockProviderRequest(t *testing.T, api config.APIMode, body map[string]any) {
	t.Helper()
	if body["model"] != "mock-model" || body["stream"] != true {
		t.Fatalf("unexpected common provider request fields: %#v", body)
	}
	if body["temperature"] != 0.2 {
		t.Fatalf("unexpected request temperature: %#v", body["temperature"])
	}

	messageKey := "input"
	maxTokensKey := "max_output_tokens"
	if api == config.APIChatCompletions {
		messageKey = "messages"
		maxTokensKey = "max_tokens"
		streamOptions, ok := body["stream_options"].(map[string]any)
		if !ok || streamOptions["include_usage"] != true {
			t.Fatalf("chat request did not request stream usage: %#v", body)
		}
	}
	if body[maxTokensKey] != float64(8192) {
		t.Fatalf("unexpected max output tokens: %#v", body[maxTokensKey])
	}
	messages, ok := body[messageKey].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("unexpected provider messages: %#v", body[messageKey])
	}
	message, ok := messages[0].(map[string]any)
	if !ok || message["role"] != "user" || message["content"] != "hi" {
		t.Fatalf("unexpected user message: %#v", messages[0])
	}
}
