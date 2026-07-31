package session

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type MessageID string
type CheckpointID string

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
	TurnID    TurnID                `json:"turn_id"`
	Sequence  int64                 `json:"sequence"`
	Role      MessageRole           `json:"role"`
	Content   string                `json:"content"`
	CreatedAt time.Time             `json:"created_at"`
}

func NewMessage(id MessageID, sessionID ConversationSessionID, turnID TurnID, sequence int64, role MessageRole, content string, createdAt time.Time) (Message, error) {
	message := Message{
		ID: id, SessionID: sessionID, TurnID: turnID, Sequence: sequence,
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
	if err := validateID("message turn", string(message.TurnID)); err != nil {
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

type CheckpointReason string

const (
	CheckpointRunStarted    CheckpointReason = "run_started"
	CheckpointToolCompleted CheckpointReason = "tool_completed"
	CheckpointStepCompleted CheckpointReason = "step_completed"
	CheckpointUserCancelled CheckpointReason = "user_cancelled"
	CheckpointRunCompleted  CheckpointReason = "run_completed"
	CheckpointRunFailed     CheckpointReason = "run_failed"
)

func (reason CheckpointReason) Valid() bool {
	switch reason {
	case CheckpointRunStarted, CheckpointToolCompleted, CheckpointStepCompleted, CheckpointUserCancelled, CheckpointRunCompleted, CheckpointRunFailed:
		return true
	default:
		return false
	}
}

type Checkpoint struct {
	ID            CheckpointID     `json:"id"`
	RunID         RunID            `json:"run_id"`
	Sequence      int64            `json:"sequence"`
	SchemaVersion int              `json:"schema_version"`
	Reason        CheckpointReason `json:"reason"`
	PayloadJSON   json.RawMessage  `json:"payload_json"`
	PayloadHash   string           `json:"payload_hash"`
	CreatedAt     time.Time        `json:"created_at"`
}

func (checkpoint Checkpoint) Validate() error {
	if err := validateID("checkpoint", string(checkpoint.ID)); err != nil {
		return err
	}
	if err := validateID("checkpoint run", string(checkpoint.RunID)); err != nil {
		return err
	}
	if checkpoint.Sequence < 1 {
		return errors.New("checkpoint sequence must be at least one")
	}
	if checkpoint.SchemaVersion < 1 {
		return errors.New("checkpoint schema version must be at least one")
	}
	if !checkpoint.Reason.Valid() {
		return fmt.Errorf("checkpoint reason %q is invalid", checkpoint.Reason)
	}
	if len(checkpoint.PayloadJSON) == 0 || !json.Valid(checkpoint.PayloadJSON) {
		return errors.New("checkpoint payload_json is empty or invalid")
	}
	decoded, err := hex.DecodeString(checkpoint.PayloadHash)
	if err != nil || len(decoded) != 32 || checkpoint.PayloadHash != strings.ToLower(checkpoint.PayloadHash) {
		return errors.New("checkpoint payload hash is invalid")
	}
	if checkpoint.CreatedAt.IsZero() {
		return errors.New("checkpoint created_at is zero")
	}
	return nil
}

type CheckpointInstruction struct {
	CheckpointID CheckpointID `json:"checkpoint_id"`
	Precedence   int          `json:"precedence"`
	Path         string       `json:"path"`
	ScopePath    string       `json:"scope_path"`
	ContentHash  string       `json:"content_hash"`
}

func (instruction CheckpointInstruction) Validate() error {
	if err := validateID("checkpoint instruction", string(instruction.CheckpointID)); err != nil {
		return err
	}
	if instruction.Precedence < 0 {
		return errors.New("checkpoint instruction precedence cannot be negative")
	}
	if strings.TrimSpace(instruction.Path) == "" || strings.TrimSpace(instruction.ScopePath) == "" {
		return errors.New("checkpoint instruction path or scope is empty")
	}
	decoded, err := hex.DecodeString(instruction.ContentHash)
	if err != nil || len(decoded) != 32 || instruction.ContentHash != strings.ToLower(instruction.ContentHash) {
		return errors.New("checkpoint instruction content hash is invalid")
	}
	return nil
}
