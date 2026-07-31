package session

import (
	"testing"
	"time"
)

func TestConversationSummaryVerifiesContentAndSourceRange(t *testing.T) {
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	project, err := NewProject("project-1", "/tmp/project", "project", now)
	if err != nil {
		t.Fatal(err)
	}
	_ = project
	sessionID := ConversationSessionID("session-1")
	messages := []Message{}
	for index, role := range []MessageRole{MessageUser, MessageAssistant} {
		message, err := NewMessage(MessageID("message-"+string(rune('1'+index))), sessionID, TurnID("turn-1"), int64(index+1), role, "content", now)
		if err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
	}
	sourceHash, err := ConversationSourceHash(messages)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := NewConversationSummary("summary-1", sessionID, 1, 2, "a derived summary", sourceHash, "deterministic", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := summary.Validate(); err != nil {
		t.Fatal(err)
	}
}
