package llm

import "testing"

func TestNewRequestCopiesMessages(t *testing.T) {
	messages := []Message{UserMessage("original")}
	request := NewRequest("test-model", messages)
	messages[0] = UserMessage("changed")

	if request.Model != "test-model" {
		t.Fatalf("unexpected model: got %q", request.Model)
	}
	if request.Messages[0].Content != "original" {
		t.Fatalf("request messages share caller storage: got %q", request.Messages[0].Content)
	}
}
