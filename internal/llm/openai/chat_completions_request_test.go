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

func TestChatCompletionsRequestSerializesCompatibleTextFields(t *testing.T) {
	var requestBody map[string]any
	provider := config.Default().Providers[config.DefaultProviderName]
	provider.APIKey = "test-secret"
	provider.BaseURL = "https://chat.example.invalid/v1"
	provider.MaxRetries = 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://chat.example.invalid/v1/chat/completions" {
			t.Fatalf("unexpected chat request URL: %s", request.URL)
		}
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode chat request body: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_test","object":"chat.completion","created":0,"model":"test-model","choices":[]}`)),
		}, nil
	})}
	client, err := newClient(provider, httpClient)
	if err != nil {
		t.Fatalf("create SDK client: %v", err)
	}

	domainRequest := llm.Request{
		Model: "test-model",
		Messages: []llm.Message{
			llm.SystemMessage("system prompt"),
			llm.DeveloperMessage("developer prompt"),
			llm.UserMessage("hello"),
			{Role: llm.RoleAssistant, Content: "previous answer", Reasoning: "provider-only reasoning"},
		},
		Temperature:     0.4,
		MaxOutputTokens: 4096,
	}
	params, err := newChatCompletionsRequest(domainRequest)
	if err != nil {
		t.Fatalf("convert chat completions request: %v", err)
	}
	if _, err := client.Chat.Completions.New(context.Background(), params); err != nil {
		t.Fatalf("send chat completions request: %v", err)
	}

	if requestBody["model"] != "test-model" {
		t.Fatalf("unexpected model: %#v", requestBody["model"])
	}
	if requestBody["temperature"] != 0.4 {
		t.Fatalf("unexpected temperature: %#v", requestBody["temperature"])
	}
	if requestBody["max_tokens"] != float64(4096) {
		t.Fatalf("unexpected max tokens: %#v", requestBody["max_tokens"])
	}
	if _, exists := requestBody["max_completion_tokens"]; exists {
		t.Fatalf("compatible request unexpectedly used max_completion_tokens: %#v", requestBody)
	}

	messages, ok := requestBody["messages"].([]any)
	if !ok || len(messages) != 4 {
		t.Fatalf("unexpected chat messages: %#v", requestBody["messages"])
	}
	expected := []struct {
		role    string
		content string
	}{
		{role: "system", content: "system prompt"},
		{role: "developer", content: "developer prompt"},
		{role: "user", content: "hello"},
		{role: "assistant", content: "previous answer"},
	}
	for index, expectation := range expected {
		message, ok := messages[index].(map[string]any)
		if !ok {
			t.Fatalf("unexpected message %d: %#v", index, messages[index])
		}
		if message["role"] != expectation.role || message["content"] != expectation.content {
			t.Fatalf("unexpected message %d: %#v", index, message)
		}
		if _, exists := message["reasoning_content"]; exists {
			t.Fatalf("provider reasoning leaked into standard chat request: %#v", message)
		}
	}
}

func TestChatCompletionsRequestRejectsUnsupportedInput(t *testing.T) {
	tests := []struct {
		name       string
		request    llm.Request
		errorMatch string
	}{
		{name: "missing model", request: validChatCompletionsDomainRequest(), errorMatch: "model"},
		{name: "missing messages", request: llm.Request{Model: "model", Temperature: 0.2, MaxOutputTokens: 10}, errorMatch: "messages"},
		{name: "temperature below range", request: llm.Request{Model: "model", Messages: []llm.Message{llm.UserMessage("hello")}, Temperature: -0.1, MaxOutputTokens: 10}, errorMatch: "temperature"},
		{name: "temperature above range", request: llm.Request{Model: "model", Messages: []llm.Message{llm.UserMessage("hello")}, Temperature: 2.1, MaxOutputTokens: 10}, errorMatch: "temperature"},
		{name: "missing max tokens", request: llm.Request{Model: "model", Messages: []llm.Message{llm.UserMessage("hello")}, Temperature: 0.2}, errorMatch: "max output tokens"},
		{name: "tool role", request: llm.Request{Model: "model", Messages: []llm.Message{{Role: llm.RoleTool, Content: "result"}}, Temperature: 0.2, MaxOutputTokens: 10}, errorMatch: "tool messages"},
		{name: "unknown role", request: llm.Request{Model: "model", Messages: []llm.Message{{Role: llm.Role("observer"), Content: "hello"}}, Temperature: 0.2, MaxOutputTokens: 10}, errorMatch: "unsupported role"},
	}
	tests[0].request.Model = ""

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newChatCompletionsRequest(test.request)
			if err == nil {
				t.Fatal("expected chat completions conversion error")
			}
			if !strings.Contains(err.Error(), test.errorMatch) {
				t.Fatalf("unexpected chat completions conversion error: %v", err)
			}
		})
	}
}

func validChatCompletionsDomainRequest() llm.Request {
	return llm.Request{
		Model:           "model",
		Messages:        []llm.Message{llm.UserMessage("hello")},
		Temperature:     0.2,
		MaxOutputTokens: 10,
	}
}
