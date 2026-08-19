package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestQwenChatDialectSerializesThinkingWithoutReasoningHistory(t *testing.T) {
	enabled := true
	request := dialectChatRequest()
	request.Reasoning = &llm.ReasoningConfig{Enabled: &enabled}
	request.Prompt.Input[0].Reasoning = "do not replay this"

	body := marshalDialectChatRequest(t, config.DialectQwen, request)
	if body["enable_thinking"] != true {
		t.Fatalf("Qwen request did not enable thinking: %#v", body)
	}
	assertCompatibleTokenAndToolFields(t, body)
	assistant := body["messages"].([]any)[0].(map[string]any)
	if _, ok := assistant["reasoning_content"]; ok {
		t.Fatalf("Qwen request replayed reasoning history: %#v", assistant)
	}
}

func TestQwenChatDialectRejectsUnsupportedPreserveOption(t *testing.T) {
	preserve := true
	request := dialectChatRequest()
	request.Reasoning = &llm.ReasoningConfig{Preserve: &preserve}
	dialect := mustResolveDialect(t, config.DialectQwen)

	_, err := newChatCompletionsRequestForDialect(request, dialect)
	if err == nil || !strings.Contains(err.Error(), "preserve") {
		t.Fatalf("unexpected Qwen preserve error: %v", err)
	}
}

func TestGLMChatDialectSerializesPreservedThinking(t *testing.T) {
	enabled := true
	preserve := true
	request := dialectChatRequest()
	request.Reasoning = &llm.ReasoningConfig{Enabled: &enabled, Preserve: &preserve}
	request.Prompt.Input[0].Reasoning = "preserved reasoning"

	body := marshalDialectChatRequest(t, config.DialectGLM, request)
	thinking := body["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || thinking["clear_thinking"] != false {
		t.Fatalf("unexpected GLM thinking options: %#v", thinking)
	}
	assertCompatibleTokenAndToolFields(t, body)
	assistant := body["messages"].([]any)[0].(map[string]any)
	if assistant["reasoning_content"] != "preserved reasoning" {
		t.Fatalf("GLM request did not preserve reasoning history: %#v", assistant)
	}
}

func TestGLMChatDialectRejectsPreserveWhenDisabled(t *testing.T) {
	enabled := false
	preserve := true
	request := dialectChatRequest()
	request.Reasoning = &llm.ReasoningConfig{Enabled: &enabled, Preserve: &preserve}
	dialect := mustResolveDialect(t, config.DialectGLM)

	_, err := newChatCompletionsRequestForDialect(request, dialect)
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("unexpected GLM reasoning error: %v", err)
	}
}

func TestDeepSeekChatDialectPreservesThinkingHistory(t *testing.T) {
	request := dialectChatRequest()
	request.Prompt.Input[0].Reasoning = "preserved DeepSeek reasoning"

	body := marshalDialectChatRequest(t, config.DialectDeepSeek, request)
	assertCompatibleTokenAndToolFields(t, body)
	assistant := body["messages"].([]any)[0].(map[string]any)
	if assistant["reasoning_content"] != "preserved DeepSeek reasoning" {
		t.Fatalf("DeepSeek request did not preserve reasoning history: %#v", assistant)
	}
	if _, ok := body["enable_thinking"]; ok {
		t.Fatalf("DeepSeek request used Qwen field: %#v", body)
	}
	if _, ok := body["thinking"]; ok {
		t.Fatalf("DeepSeek request used GLM field: %#v", body)
	}
}

func TestStandardChatDialectPreservesThinkingHistory(t *testing.T) {
	request := dialectChatRequest()
	request.Prompt.Input[0].Reasoning = "preserved custom-provider reasoning"

	body := marshalDialectChatRequest(t, config.DialectStandard, request)
	assistant := body["messages"].([]any)[0].(map[string]any)
	if assistant["reasoning_content"] != "preserved custom-provider reasoning" {
		t.Fatalf("standard request did not preserve reasoning history: %#v", assistant)
	}
}

func dialectChatRequest() llm.Request {
	return llm.Request{
		Model: "test-model",
		Prompt: llm.Prompt{
			Input: []llm.ResponseItem{
				llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call_1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)}),
				llm.ToolResultMessage("call_1", "contents"),
			},
			Tools: []llm.ToolSpec{{
				Name: "read", Description: "Read a file", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`), Strict: false,
			}},
		},
	}
}

func marshalDialectChatRequest(t *testing.T, name config.ProviderDialect, request llm.Request) map[string]any {
	t.Helper()
	dialect := mustResolveDialect(t, name)
	params, err := newChatCompletionsRequestForDialect(request, dialect)
	if err != nil {
		t.Fatalf("convert %s request: %v", name, err)
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal %s request: %v", name, err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatalf("decode %s request: %v", name, err)
	}
	return body
}

func mustResolveDialect(t *testing.T, name config.ProviderDialect) Dialect {
	t.Helper()
	dialect, err := resolveDialect(name)
	if err != nil {
		t.Fatalf("resolve dialect %q: %v", name, err)
	}
	return dialect
}

func assertCompatibleTokenAndToolFields(t *testing.T, body map[string]any) {
	t.Helper()
	if body["max_tokens"] != float64(256) {
		t.Fatalf("unexpected max_tokens: %#v", body)
	}
	if _, ok := body["max_completion_tokens"]; ok {
		t.Fatalf("compatible dialect emitted max_completion_tokens: %#v", body)
	}
	tool := body["tools"].([]any)[0].(map[string]any)
	function := tool["function"].(map[string]any)
	if tool["type"] != "function" || function["name"] != "read" {
		t.Fatalf("unexpected compatible tool definition: %#v", tool)
	}
	if _, ok := function["strict"]; ok {
		t.Fatalf("compatible dialect emitted unsupported strict field: %#v", function)
	}
}
