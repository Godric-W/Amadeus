package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	provider.RequestMaxRetries = 0
	client, err := newSDKClient(provider, nil)
	if err != nil {
		t.Fatalf("create SDK client: %v", err)
	}

	domainRequest := llm.Request{
		Model: "test-model",
		Prompt: llm.Prompt{Input: []llm.ResponseItem{
			llm.DeveloperMessage("developer prompt"), llm.UserMessage("hello"),
			{Role: llm.RoleAssistant, Content: "previous answer", Reasoning: "provider-only reasoning"},
		}, BaseInstructions: llm.BaseInstructions{Text: "system prompt"}},
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

func TestResponsesRequestSerializesStandardToolProtocol(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{"id":"resp_tools"}`)
	}))
	defer server.Close()

	provider := config.Default().Providers[config.DefaultProviderName]
	provider.APIKey = "test-secret"
	provider.BaseURL = server.URL + "/v1"
	provider.RequestMaxRetries = 0
	client, err := newSDKClient(provider, nil)
	if err != nil {
		t.Fatalf("create SDK client: %v", err)
	}

	domainRequest := llm.Request{
		Model: "test-model",
		Prompt: llm.Prompt{
			Input: []llm.ResponseItem{
				llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call_1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)}),
				llm.ToolResultMessage("call_1", "file contents"),
			},
			Tools: []llm.ToolSpec{{
				Name: "read", Description: "Read a file", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`), Strict: true,
			}},
		},
		Temperature: 0.2, MaxOutputTokens: 100,
	}
	params, err := newResponsesRequest(domainRequest)
	if err != nil {
		t.Fatalf("convert responses request: %v", err)
	}
	if _, err := client.Responses.New(context.Background(), params); err != nil {
		t.Fatalf("send responses request: %v", err)
	}

	tools := requestBody["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "read" || tool["description"] != "Read a file" || tool["strict"] != true {
		t.Fatalf("unexpected Responses tool: %#v", tool)
	}
	input := requestBody["input"].([]any)
	call := input[0].(map[string]any)
	result := input[1].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "call_1" || call["name"] != "read" || call["arguments"] != `{"path":"README.md"}` {
		t.Fatalf("unexpected Responses tool call: %#v", call)
	}
	if result["type"] != "function_call_output" || result["call_id"] != "call_1" || result["output"] != "file contents" {
		t.Fatalf("unexpected Responses tool result: %#v", result)
	}
}

func TestResponsesRequestSerializesUserAndToolImagesAsContentParts(t *testing.T) {
	var requestBody map[string]any
	provider := config.Default().Providers[config.DefaultProviderName]
	provider.APIKey = "test-secret"
	provider.BaseURL = "https://responses.example.invalid/v1"
	provider.RequestMaxRetries = 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode Responses image request: %v", err)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_images"}`))}, nil
	})}
	client, err := newSDKClient(provider, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	request := llm.Request{
		Model: "test-model", Temperature: 0.2, MaxOutputTokens: 100,
		Prompt: llm.Prompt{Input: []llm.ResponseItem{
			{Role: llm.RoleUser, Content: "inspect", Parts: []llm.ContentPart{llm.ImagePart("image/png", "YQ==")}},
			llm.ToolResultMessageWithParts("call_image", "tool image", llm.ImagePart("image/jpeg", "Yg==")),
		}},
	}
	params, err := newResponsesRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Responses.New(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	input := requestBody["input"].([]any)
	userContent := input[0].(map[string]any)["content"].([]any)
	if len(userContent) != 2 || userContent[0].(map[string]any)["type"] != "input_text" || userContent[1].(map[string]any)["type"] != "input_image" || userContent[1].(map[string]any)["image_url"] != "data:image/png;base64,YQ==" {
		t.Fatalf("unexpected Responses user image content: %#v", userContent)
	}
	output := input[1].(map[string]any)["output"].([]any)
	if len(output) != 2 || output[0].(map[string]any)["type"] != "input_text" || output[1].(map[string]any)["type"] != "input_image" || output[1].(map[string]any)["image_url"] != "data:image/jpeg;base64,Yg==" {
		t.Fatalf("unexpected Responses tool image output: %#v", output)
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
		{name: "temperature below range", request: llm.Request{Model: "model", Prompt: llm.Prompt{Input: []llm.ResponseItem{llm.UserMessage("hello")}}, Temperature: -0.1, MaxOutputTokens: 10}, errorMatch: "temperature"},
		{name: "temperature above range", request: llm.Request{Model: "model", Prompt: llm.Prompt{Input: []llm.ResponseItem{llm.UserMessage("hello")}}, Temperature: 2.1, MaxOutputTokens: 10}, errorMatch: "temperature"},
		{name: "missing max tokens", request: llm.Request{Model: "model", Prompt: llm.Prompt{Input: []llm.ResponseItem{llm.UserMessage("hello")}}, Temperature: 0.2}, errorMatch: "max output tokens"},
		{name: "tool role missing call ID", request: llm.Request{Model: "model", Prompt: llm.Prompt{Input: []llm.ResponseItem{{Role: llm.RoleTool, Content: "result"}}}, Temperature: 0.2, MaxOutputTokens: 10}, errorMatch: "call ID"},
		{name: "unknown role", request: llm.Request{Model: "model", Prompt: llm.Prompt{Input: []llm.ResponseItem{{Role: llm.Role("observer"), Content: "hello"}}}, Temperature: 0.2, MaxOutputTokens: 10}, errorMatch: "unsupported role"},
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
		Prompt:          llm.Prompt{Input: []llm.ResponseItem{llm.UserMessage("hello")}},
		Temperature:     0.2,
		MaxOutputTokens: 10,
	}
}
