package openai

import (
	"encoding/json"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/react"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestToolResultReplayPreservesCallOrderAcrossProtocols(t *testing.T) {
	assistant := llm.AssistantToolCallMessage("",
		llm.ToolCall{ID: "call_1", Name: "first", Arguments: json.RawMessage(`{"value":1}`)},
		llm.ToolCall{ID: "call_2", Name: "second", Arguments: json.RawMessage(`{"value":2}`)},
	)
	outcomes := []react.ToolOutcome{
		toolReplayExecution("call_2", "second", "two"),
		toolReplayExecution("call_1", "first", "one"),
	}
	messages, err := react.ReplayToolResults(assistant, outcomes)
	if err != nil {
		t.Fatalf("build replay messages: %v", err)
	}
	request := llm.Request{Model: "test-model", Messages: messages, Temperature: 0.2, MaxOutputTokens: 100}

	responsesParams, err := newResponsesRequest(request)
	if err != nil {
		t.Fatalf("convert Responses replay: %v", err)
	}
	responsesBody := marshalRequestBody(t, responsesParams)
	input := responsesBody["input"].([]any)
	if len(input) != 4 || input[0].(map[string]any)["call_id"] != "call_1" || input[1].(map[string]any)["call_id"] != "call_2" || input[2].(map[string]any)["call_id"] != "call_1" || input[3].(map[string]any)["call_id"] != "call_2" {
		t.Fatalf("unexpected Responses replay order: %#v", input)
	}

	chatParams, err := newChatCompletionsRequest(request)
	if err != nil {
		t.Fatalf("convert Chat replay: %v", err)
	}
	chatBody := marshalRequestBody(t, chatParams)
	chatMessages := chatBody["messages"].([]any)
	toolCalls := chatMessages[0].(map[string]any)["tool_calls"].([]any)
	if len(chatMessages) != 3 || toolCalls[0].(map[string]any)["id"] != "call_1" || toolCalls[1].(map[string]any)["id"] != "call_2" || chatMessages[1].(map[string]any)["tool_call_id"] != "call_1" || chatMessages[2].(map[string]any)["tool_call_id"] != "call_2" {
		t.Fatalf("unexpected Chat replay order: %#v", chatMessages)
	}
}

func toolReplayExecution(callID, toolName, text string) react.ToolOutcome {
	return react.ToolOutcome{CallID: callID, ToolName: toolName, Status: react.ToolOutcomeSucceeded, Result: tool.Result{CallID: callID, ToolName: toolName, Text: text}}
}

func marshalRequestBody(t *testing.T, value any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return body
}
