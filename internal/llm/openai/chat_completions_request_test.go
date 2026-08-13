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
	client, err := newSDKClient(provider, httpClient)
	if err != nil {
		t.Fatalf("create SDK client: %v", err)
	}

	domainRequest := llm.Request{
		Model: "test-model",
		Prompt: llm.Prompt{BaseInstructions: llm.BaseInstructions{Text: "system prompt"}, Input: []llm.Message{
			llm.DeveloperMessage("developer prompt"), llm.UserMessage("hello"),
			{Role: llm.RoleAssistant, Content: "previous answer", Reasoning: "provider-only reasoning"},
		}},
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
		{role: "system", content: "developer prompt"},
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

func TestChatCompletionsRequestPreservesDeveloperRoleWhenDialectSupportsIt(t *testing.T) {
	dialect, err := resolveDialect(config.DialectOpenAI)
	if err != nil {
		t.Fatalf("resolve OpenAI dialect: %v", err)
	}
	providerCapabilities := dialect.Capabilities(config.APIChatCompletions)
	if providerCapabilities.SupportsDeveloperRole {
		params, err := newChatCompletionsRequestForDialect(llm.Request{
			Model: "test-model", Prompt: llm.Prompt{Input: []llm.Message{llm.DeveloperMessage("developer prompt")}},
			MaxOutputTokens: 128,
		}, dialect)
		if err != nil {
			t.Fatalf("convert OpenAI chat request: %v", err)
		}
		if len(params.Messages) != 1 || params.Messages[0].OfDeveloper == nil {
			t.Fatalf("developer role was not preserved: %#v", params.Messages)
		}
	}
}

func TestChatCompletionsRequestSerializesStandardToolProtocol(t *testing.T) {
	var requestBody map[string]any
	provider := config.Default().Providers[config.DefaultProviderName]
	provider.APIKey = "test-secret"
	provider.BaseURL = "https://chat.example.invalid/v1"
	provider.MaxRetries = 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode chat request body: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_tools","object":"chat.completion","created":0,"model":"test-model","choices":[]}`)),
		}, nil
	})}
	client, err := newSDKClient(provider, httpClient)
	if err != nil {
		t.Fatalf("create SDK client: %v", err)
	}

	domainRequest := llm.Request{
		Model: "test-model",
		Prompt: llm.Prompt{
			Input: []llm.Message{
				llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call_1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)}),
				llm.ToolResultMessage("call_1", "file contents"),
			},
			Tools: []llm.ToolDefinition{{
				Name: "read", Description: "Read a file", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`), Strict: true,
			}},
		},
		Temperature: 0.2, MaxOutputTokens: 100,
	}
	params, err := newChatCompletionsRequest(domainRequest)
	if err != nil {
		t.Fatalf("convert chat completions request: %v", err)
	}
	if _, err := client.Chat.Completions.New(context.Background(), params); err != nil {
		t.Fatalf("send chat completions request: %v", err)
	}

	tools := requestBody["tools"].([]any)
	function := tools[0].(map[string]any)["function"].(map[string]any)
	if function["name"] != "read" || function["description"] != "Read a file" || function["strict"] != true {
		t.Fatalf("unexpected Chat tool: %#v", tools[0])
	}
	messages := requestBody["messages"].([]any)
	assistant := messages[0].(map[string]any)
	call := assistant["tool_calls"].([]any)[0].(map[string]any)
	callFunction := call["function"].(map[string]any)
	if assistant["role"] != "assistant" || call["id"] != "call_1" || call["type"] != "function" || callFunction["name"] != "read" || callFunction["arguments"] != `{"path":"README.md"}` {
		t.Fatalf("unexpected Chat tool call: %#v", assistant)
	}
	result := messages[1].(map[string]any)
	if result["role"] != "tool" || result["tool_call_id"] != "call_1" || result["content"] != "file contents" {
		t.Fatalf("unexpected Chat tool result: %#v", result)
	}
}

func TestChatCompletionsRequestSerializesUserAndSyntheticToolImages(t *testing.T) {
	var requestBody map[string]any
	provider := config.Default().Providers[config.DefaultProviderName]
	provider.APIKey = "test-secret"
	provider.BaseURL = "https://chat.example.invalid/v1"
	provider.MaxRetries = 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode Chat image request: %v", err)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"chat_images","object":"chat.completion","created":0,"model":"test-model","choices":[]}`))}, nil
	})}
	client, err := newSDKClient(provider, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	domainRequest := llm.Request{
		Model: "test-model", Temperature: 0.2, MaxOutputTokens: 100,
		Prompt: llm.Prompt{Input: []llm.Message{
			{Role: llm.RoleUser, Content: "inspect", Parts: []llm.ContentPart{llm.ImagePart("image/png", "YQ==")}},
			llm.ToolResultMessageWithParts("call_image", "tool image", llm.ImagePart("image/jpeg", "Yg==")),
		}},
	}
	params, err := newChatCompletionsRequest(domainRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Chat.Completions.New(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	messages := requestBody["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("unexpected Chat image message count: %#v", messages)
	}
	userContent := messages[0].(map[string]any)["content"].([]any)
	if len(userContent) != 2 || userContent[1].(map[string]any)["type"] != "image_url" || userContent[1].(map[string]any)["image_url"].(map[string]any)["url"] != "data:image/png;base64,YQ==" {
		t.Fatalf("unexpected Chat user image content: %#v", userContent)
	}
	toolMessage := messages[1].(map[string]any)
	if toolMessage["role"] != "tool" || toolMessage["content"] != "tool image" {
		t.Fatalf("unexpected Chat tool text message: %#v", toolMessage)
	}
	synthetic := messages[2].(map[string]any)
	syntheticContent := synthetic["content"].([]any)
	if synthetic["role"] != "user" || len(syntheticContent) != 2 || syntheticContent[1].(map[string]any)["image_url"].(map[string]any)["url"] != "data:image/jpeg;base64,Yg==" {
		t.Fatalf("unexpected synthetic Chat image message: %#v", synthetic)
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
		{name: "temperature below range", request: llm.Request{Model: "model", Prompt: llm.Prompt{Input: []llm.Message{llm.UserMessage("hello")}}, Temperature: -0.1, MaxOutputTokens: 10}, errorMatch: "temperature"},
		{name: "temperature above range", request: llm.Request{Model: "model", Prompt: llm.Prompt{Input: []llm.Message{llm.UserMessage("hello")}}, Temperature: 2.1, MaxOutputTokens: 10}, errorMatch: "temperature"},
		{name: "missing max tokens", request: llm.Request{Model: "model", Prompt: llm.Prompt{Input: []llm.Message{llm.UserMessage("hello")}}, Temperature: 0.2}, errorMatch: "max output tokens"},
		{name: "tool role missing call ID", request: llm.Request{Model: "model", Prompt: llm.Prompt{Input: []llm.Message{{Role: llm.RoleTool, Content: "result"}}}, Temperature: 0.2, MaxOutputTokens: 10}, errorMatch: "call ID"},
		{name: "unknown role", request: llm.Request{Model: "model", Prompt: llm.Prompt{Input: []llm.Message{{Role: llm.Role("observer"), Content: "hello"}}}, Temperature: 0.2, MaxOutputTokens: 10}, errorMatch: "unsupported role"},
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
		Prompt:          llm.Prompt{Input: []llm.Message{llm.UserMessage("hello")}},
		Temperature:     0.2,
		MaxOutputTokens: 10,
	}
}
