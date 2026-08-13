package llm

import "testing"

func TestNewRequestCopiesMessages(t *testing.T) {
	messages := []Message{AssistantToolCallMessage("", ToolCall{ID: "call_1", Name: "read", Arguments: []byte(`{"path":"a"}`)})}
	request := NewRequest("test-model", messages)

	if request.Model != "test-model" {
		t.Fatalf("unexpected model: got %q", request.Model)
	}
	messages[0].ToolCalls[0].Name = "changed"
	messages[0].ToolCalls[0].Arguments[0] = '['
	if request.Prompt.Input[0].ToolCalls[0].Name != "read" || string(request.Prompt.Input[0].ToolCalls[0].Arguments) != `{"path":"a"}` {
		t.Fatalf("request Prompt input shares caller storage: %#v", request.Prompt.Input[0])
	}
}
