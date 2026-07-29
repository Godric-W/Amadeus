package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestResponsesRequestSerializesDomainTextFields(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Errorf("unexpected request path: %s", request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{"id":"resp_test"}`)
	}))
	defer server.Close()

	provider := config.Default().Providers[config.DefaultProviderName]
	provider.APIKey = "test-secret"
	provider.BaseURL = server.URL + "/v1"
	provider.MaxRetries = 0
	client, err := NewClient(provider)
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
		Temperature:     0.3,
		MaxOutputTokens: 2048,
	}
	params, err := newResponsesRequest(domainRequest)
	if err != nil {
		t.Fatalf("convert responses request: %v", err)
	}
	if _, err := client.Responses.New(context.Background(), params); err != nil {
		t.Fatalf("send responses request: %v", err)
	}

	if requestBody["model"] != "test-model" {
		t.Fatalf("unexpected model: %#v", requestBody["model"])
	}
	if requestBody["temperature"] != 0.3 {
		t.Fatalf("unexpected temperature: %#v", requestBody["temperature"])
	}
	if requestBody["max_output_tokens"] != float64(2048) {
		t.Fatalf("unexpected max output tokens: %#v", requestBody["max_output_tokens"])
	}

	messages, ok := requestBody["input"].([]any)
	if !ok || len(messages) != 4 {
		t.Fatalf("unexpected input messages: %#v", requestBody["input"])
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
			t.Fatalf("provider reasoning leaked into Responses input: %#v", message)
		}
	}
}

func TestResponsesRequestRejectsUnsupportedInput(t *testing.T) {
	tests := []struct {
		name       string
		request    llm.Request
		errorMatch string
	}{
		{name: "missing model", request: validResponsesDomainRequest(), errorMatch: "model"},
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
			_, err := newResponsesRequest(test.request)
			if err == nil {
				t.Fatal("expected responses conversion error")
			}
			if !strings.Contains(err.Error(), test.errorMatch) {
				t.Fatalf("unexpected responses conversion error: %v", err)
			}
		})
	}
}

func validResponsesDomainRequest() llm.Request {
	return llm.Request{
		Model:           "model",
		Messages:        []llm.Message{llm.UserMessage("hello")},
		Temperature:     0.2,
		MaxOutputTokens: 10,
	}
}
