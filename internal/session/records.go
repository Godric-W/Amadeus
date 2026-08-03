package session

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type MessageID string

type MessageRole string

const (
	MessageUser      MessageRole = "user"
	MessageAssistant MessageRole = "assistant"
)

func (role MessageRole) Valid() bool {
	return role == MessageUser || role == MessageAssistant
}

type Message struct {
	ID        MessageID             `json:"id"`
	SessionID ConversationSessionID `json:"session_id"`
	RunID     RunID                 `json:"run_id"`
	Sequence  int64                 `json:"sequence"`
	Role      MessageRole           `json:"role"`
	Content   string                `json:"content"`
	CreatedAt time.Time             `json:"created_at"`
}

func NewMessage(id MessageID, sessionID ConversationSessionID, runID RunID, sequence int64, role MessageRole, content string, createdAt time.Time) (Message, error) {
	message := Message{
		ID: id, SessionID: sessionID, RunID: runID, Sequence: sequence,
		Role: role, Content: strings.TrimSpace(content), CreatedAt: createdAt.UTC(),
	}
	return message, message.Validate()
}

func (message Message) Validate() error {
	if err := validateID("message", string(message.ID)); err != nil {
		return err
	}
	if err := validateID("message session", string(message.SessionID)); err != nil {
		return err
	}
	if err := validateID("message run", string(message.RunID)); err != nil {
		return err
	}
	if message.Sequence < 1 {
		return errors.New("message sequence must be at least one")
	}
	if !message.Role.Valid() {
		return fmt.Errorf("message role %q is invalid", message.Role)
	}
	if strings.TrimSpace(message.Content) == "" {
		return errors.New("message content is empty")
	}
	if message.CreatedAt.IsZero() {
		return errors.New("message created_at is zero")
	}
	return nil
}
