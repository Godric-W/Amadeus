package openai

import (
	"encoding/json"
	"testing"

	contextmanager "github.com/Godric-W/Amadeus/internal/contextmanager"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestCanonicalToolProjectionPreservesCallOrderAcrossProtocols(t *testing.T) {
	lines := []rollout.Line{
		toolProjectionLine(t, 1, rollout.ResponseItem{Type: rollout.ResponseToolCall, Role: "assistant", CallID: "call_1", Name: "first", Arguments: json.RawMessage(`{"value":1}`)}),
		toolProjectionLine(t, 2, rollout.ResponseItem{Type: rollout.ResponseToolCall, Role: "assistant", CallID: "call_2", Name: "second", Arguments: json.RawMessage(`{"value":2}`)}),
		toolProjectionLine(t, 3, rollout.ResponseItem{Type: rollout.ResponseToolResult, Role: "tool", CallID: "call_1", Name: "first", Status: "succeeded", Result: &tool.ToolResult{CallID: "call_1", ToolName: "first", Text: "one"}}),
		toolProjectionLine(t, 4, rollout.ResponseItem{Type: rollout.ResponseToolResult, Role: "tool", CallID: "call_2", Name: "second", Status: "succeeded", Result: &tool.ToolResult{CallID: "call_2", ToolName: "second", Text: "two"}}),
	}
	projection, err := contextmanager.ProjectRolloutMessages(lines)
	if err != nil {
		t.Fatalf("project canonical tool messages: %v", err)
	}
	request := llm.Request{Model: "test-model", Prompt: llm.Prompt{Input: projection.Messages}}

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

func toolProjectionLine(t *testing.T, sequence uint64, payload rollout.ResponseItem) rollout.Line {
	t.Helper()
	item, err := rollout.NewResponseItem(payload)
	if err != nil {
		t.Fatal(err)
	}
	return rollout.Line{Sequence: sequence, Item: item}
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
