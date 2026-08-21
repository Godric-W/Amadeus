package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestChatDialectReasoningEffortMapping(t *testing.T) {
	tests := []struct {
		name       string
		dialect    config.ProviderDialect
		effort     *llm.ReasoningEffort
		wantEffort string
		wantField  string
		wantValue  any
	}{
		{name: "standard high", dialect: config.DialectStandard, effort: effortPointer(llm.ReasoningEffortHigh), wantEffort: "high"},
		{name: "OpenAI max", dialect: config.DialectOpenAI, effort: effortPointer(llm.ReasoningEffortMax), wantEffort: "max"},
		{name: "DeepSeek low", dialect: config.DialectDeepSeek, effort: effortPointer(llm.ReasoningEffortLow), wantEffort: "low"},
		{name: "DeepSeek none", dialect: config.DialectDeepSeek, effort: effortPointer(llm.ReasoningEffortNone), wantField: "thinking", wantValue: map[string]any{"type": "disabled"}},
		{name: "Qwen xhigh", dialect: config.DialectQwen, effort: effortPointer(llm.ReasoningEffortXHigh), wantEffort: "xhigh"},
		{name: "Qwen none", dialect: config.DialectQwen, effort: effortPointer(llm.ReasoningEffortNone), wantField: "enable_thinking", wantValue: false},
		{name: "GLM medium", dialect: config.DialectGLM, effort: effortPointer(llm.ReasoningEffortMedium), wantEffort: "medium"},
		{name: "GLM none", dialect: config.DialectGLM, effort: effortPointer(llm.ReasoningEffortNone), wantField: "thinking", wantValue: map[string]any{"type": "disabled"}},
		{name: "unset", dialect: config.DialectQwen},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := dialectChatRequest()
			request.Reasoning = llm.ReasoningConfigForEffort(test.effort)
			body := marshalDialectChatRequest(t, test.dialect, request)
			if got, ok := body["reasoning_effort"]; test.wantEffort == "" {
				if ok {
					t.Fatalf("unexpected reasoning_effort: %#v", got)
				}
			} else if !ok || got != test.wantEffort {
				t.Fatalf("reasoning_effort = %#v, want %q", got, test.wantEffort)
			}
			for _, field := range []string{"thinking", "enable_thinking"} {
				got, ok := body[field]
				if field == test.wantField {
					if !ok || !jsonValuesEqual(got, test.wantValue) {
						t.Fatalf("%s = %#v, want %#v", field, got, test.wantValue)
					}
				} else if ok {
					t.Fatalf("unexpected %s field: %#v", field, got)
				}
			}
			if test.dialect != config.DialectStandard && test.dialect != config.DialectOpenAI {
				assertCompatibleTokenAndToolFields(t, body)
			}
		})
	}
}

func TestReasoningChatDialectsReplayAssistantReasoningHistory(t *testing.T) {
	for _, dialect := range []config.ProviderDialect{config.DialectStandard, config.DialectDeepSeek, config.DialectQwen, config.DialectGLM} {
		t.Run(string(dialect), func(t *testing.T) {
			request := dialectChatRequest()
			request.Prompt.Input[0].Reasoning = "preserved reasoning"
			body := marshalDialectChatRequest(t, dialect, request)
			assistant := body["messages"].([]any)[0].(map[string]any)
			if assistant["reasoning_content"] != "preserved reasoning" {
				t.Fatalf("reasoning history was not preserved: %#v", assistant)
			}
		})
	}
}

func TestChatDialectRejectsInvalidReasoningEffort(t *testing.T) {
	invalid := llm.ReasoningEffort("maximum")
	request := dialectChatRequest()
	request.Reasoning = &llm.ReasoningConfig{Effort: &invalid}
	_, err := newChatCompletionsRequestForDialect(request, mustResolveDialect(t, config.DialectStandard))
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("unexpected invalid effort error: %v", err)
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
	if _, ok := body["max_tokens"]; ok {
		t.Fatalf("request forced max_tokens: %#v", body)
	}
	if _, ok := body["max_completion_tokens"]; ok {
		t.Fatalf("request forced max_completion_tokens: %#v", body)
	}
	tools := body["tools"].([]any)
	function := tools[0].(map[string]any)["function"].(map[string]any)
	if _, ok := function["strict"]; ok {
		t.Fatalf("compatible dialect emitted strict tool schema: %#v", function)
	}
}

func effortPointer(value llm.ReasoningEffort) *llm.ReasoningEffort { return &value }

func jsonValuesEqual(left, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}
