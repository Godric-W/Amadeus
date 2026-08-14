package rollout

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/tool"
)

const CurrentVersion = 1

type ThreadID string

type TurnID string

type Kind string

const (
	KindSessionMeta       Kind = "session_meta"
	KindTurnContext       Kind = "turn_context"
	KindTurnStarted       Kind = "turn_started"
	KindResponseItem      Kind = "response_item"
	KindTurnItemCompleted Kind = "turn_item_completed"
	KindPlanUpdate        Kind = "plan_update"
	KindCompaction        Kind = "compaction"
	KindTokenUsage        Kind = "token_usage"
	KindTurnCompleted     Kind = "turn_completed"
	KindTurnAborted       Kind = "turn_aborted"
	KindContextUpdate     Kind = "context_update"
)

type Item struct {
	Kind    Kind            `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

type Line struct {
	Version   int       `json:"version"`
	Sequence  uint64    `json:"sequence"`
	Timestamp time.Time `json:"timestamp"`
	ThreadID  ThreadID  `json:"thread_id"`
	TurnID    TurnID    `json:"turn_id,omitempty"`
	Item      Item      `json:"item"`
}

type SessionMeta struct {
	CWD           string    `json:"cwd"`
	Title         string    `json:"title"`
	ModelProvider string    `json:"model_provider,omitempty"`
	Model         string    `json:"model,omitempty"`
	GitSHA        string    `json:"git_sha,omitempty"`
	GitBranch     string    `json:"git_branch,omitempty"`
	GitOriginURL  string    `json:"git_origin_url,omitempty"`
	Archived      bool      `json:"archived,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type TurnStarted struct {
	Input string `json:"input"`
}

type ResponseItemType string

const (
	ResponseUserMessage      ResponseItemType = "user_message"
	ResponseAssistantMessage ResponseItemType = "assistant_message"
	ResponseToolCall         ResponseItemType = "tool_call"
	ResponseToolResult       ResponseItemType = "tool_result"
)

type ResponseError struct {
	Kind    string `json:"kind,omitempty"`
	Message string `json:"message"`
}

type ResponseItem struct {
	Type      ResponseItemType   `json:"type"`
	Role      string             `json:"role,omitempty"`
	Content   string             `json:"content,omitempty"`
	Reasoning string             `json:"reasoning_content,omitempty"`
	CallID    string             `json:"call_id,omitempty"`
	Name      string             `json:"name,omitempty"`
	Arguments json.RawMessage    `json:"arguments,omitempty"`
	Status    string             `json:"status,omitempty"`
	Result    *tool.ToolResult   `json:"result,omitempty"`
	Error     *ResponseError     `json:"error,omitempty"`
	Metadata  map[string]any     `json:"metadata,omitempty"`
	Partial   bool               `json:"partial,omitempty"`
	Duration  int64              `json:"duration_nanos,omitempty"`
	Parts     []tool.ContentPart `json:"parts,omitempty"`
}

type ReplacementMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Compaction struct {
	Summary                string               `json:"summary"`
	ReplacementHistory     []ReplacementMessage `json:"replacement_history"`
	CoveredThroughSequence int64                `json:"covered_through_sequence"`
	SourceHash             string               `json:"source_hash"`
	Provider               string               `json:"provider,omitempty"`
	Model                  string               `json:"model,omitempty"`
}

type TurnTerminalStatus string

const (
	TurnStatusCompleted TurnTerminalStatus = "completed"
	TurnStatusFailed    TurnTerminalStatus = "failed"
)

type TurnCompleted struct {
	Status  TurnTerminalStatus `json:"status"`
	Summary string             `json:"summary,omitempty"`
	Error   string             `json:"error,omitempty"`
}

type TurnAborted struct {
	Summary string `json:"summary,omitempty"`
	Reason  string `json:"reason"`
}

// TurnItemCompleted is the durable, self-contained projection of one user-visible
// turn item. It deliberately lives in rollout rather than importing the agent
// protocol package, keeping canonical persistence independent from the runtime.
type TurnItemCompleted struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
	CompletedAt time.Time       `json:"completed_at,omitempty"`
	Text        string          `json:"text,omitempty"`
	ToolName    string          `json:"tool_name,omitempty"`
	CallID      string          `json:"call_id,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

type ContextUpdate struct {
	Title    string `json:"title,omitempty"`
	Archived *bool  `json:"archived,omitempty"`
	Key      string `json:"key,omitempty"`
	Content  string `json:"content,omitempty"`
}

type TokenUsage struct {
	InputTokens       int64 `json:"input_tokens,omitempty"`
	CachedInputTokens int64 `json:"cached_input_tokens,omitempty"`
	OutputTokens      int64 `json:"output_tokens,omitempty"`
	ReasoningTokens   int64 `json:"reasoning_tokens,omitempty"`
	TotalTokens       int64 `json:"total_tokens"`
}

func NewItem(kind Kind, payload any) (Item, error) {
	if err := validateKind(kind); err != nil {
		return Item{}, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Item{}, fmt.Errorf("encode rollout payload %q: %w", kind, err)
	}
	item := Item{Kind: kind, Payload: encoded}
	if err := item.Validate(); err != nil {
		return Item{}, err
	}
	return item, nil
}

func NewRawItem(kind Kind, payload json.RawMessage) (Item, error) {
	item := Item{Kind: kind, Payload: append(json.RawMessage(nil), payload...)}
	if err := item.Validate(); err != nil {
		return Item{}, err
	}
	return item, nil
}

func (item Item) Validate() error {
	if err := validateKind(item.Kind); err != nil {
		return err
	}
	if len(item.Payload) == 0 || !json.Valid(item.Payload) || bytes.Equal(bytes.TrimSpace(item.Payload), []byte("null")) {
		return fmt.Errorf("rollout payload %q is invalid", item.Kind)
	}
	return validateKnownPayload(item)
}

func (line Line) Validate(expectedThreadID ThreadID, expectedSequence uint64) error {
	if line.Version < 1 || line.Version > CurrentVersion {
		return fmt.Errorf("unsupported rollout version %d", line.Version)
	}
	if line.Sequence != expectedSequence {
		return fmt.Errorf("rollout sequence is %d, expected %d", line.Sequence, expectedSequence)
	}
	if line.Timestamp.IsZero() {
		return errors.New("rollout timestamp is zero")
	}
	if err := validateID("thread", string(line.ThreadID)); err != nil {
		return err
	}
	if expectedThreadID != "" && line.ThreadID != expectedThreadID {
		return fmt.Errorf("rollout thread is %q, expected %q", line.ThreadID, expectedThreadID)
	}
	if line.TurnID != "" {
		if err := validateID("turn", string(line.TurnID)); err != nil {
			return err
		}
	}
	if err := line.Item.Validate(); err != nil {
		return err
	}
	if requiresTurn(line.Item.Kind) && line.TurnID == "" {
		return fmt.Errorf("rollout item %q requires a turn ID", line.Item.Kind)
	}
	if line.Item.Kind == KindSessionMeta && line.TurnID != "" {
		return errors.New("session_meta cannot have a turn ID")
	}
	if line.Item.Kind == KindTurnContext {
		var contextPayload struct {
			ThreadID ThreadID `json:"thread_id"`
			TurnID   TurnID   `json:"turn_id"`
			Provider string   `json:"provider"`
			Model    string   `json:"model"`
			CWD      string   `json:"cwd"`
		}
		if err := json.Unmarshal(line.Item.Payload, &contextPayload); err != nil {
			return fmt.Errorf("decode turn_context payload: %w", err)
		}
		if contextPayload.ThreadID != line.ThreadID || contextPayload.TurnID != line.TurnID {
			return errors.New("turn_context identity does not match rollout envelope")
		}
		if strings.TrimSpace(contextPayload.Provider) == "" || strings.TrimSpace(contextPayload.Model) == "" || strings.TrimSpace(contextPayload.CWD) == "" {
			return errors.New("turn_context payload is incomplete")
		}
	}
	return nil
}

func DecodePayload[T any](item Item) (T, error) {
	var payload T
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		return payload, fmt.Errorf("decode rollout payload %q: %w", item.Kind, err)
	}
	return payload, nil
}

func NewResponseItem(payload ResponseItem) (Item, error) {
	return NewItem(KindResponseItem, payload)
}

func DecodeResponseItem(item Item) (ResponseItem, error) {
	payload, err := DecodePayload[ResponseItem](item)
	if err != nil {
		return ResponseItem{}, err
	}
	if payload.Type == "" {
		switch payload.Role {
		case "user":
			payload.Type = ResponseUserMessage
		case "assistant":
			payload.Type = ResponseAssistantMessage
		}
	}
	return payload, validateResponseItem(payload)
}

func IsKnownKind(kind Kind) bool {
	switch kind {
	case KindSessionMeta, KindTurnContext, KindTurnStarted, KindResponseItem,
		KindTurnItemCompleted, KindPlanUpdate, KindCompaction, KindTokenUsage,
		KindTurnCompleted, KindTurnAborted, KindContextUpdate:
		return true
	default:
		return false
	}
}

func validateKnownPayload(item Item) error {
	switch item.Kind {
	case KindSessionMeta:
		payload, err := DecodePayload[SessionMeta](item)
		if err != nil {
			return err
		}
		if strings.TrimSpace(payload.CWD) == "" || strings.TrimSpace(payload.Title) == "" || payload.CreatedAt.IsZero() {
			return errors.New("session_meta payload is incomplete")
		}
	case KindTurnStarted:
		payload, err := DecodePayload[TurnStarted](item)
		if err != nil {
			return err
		}
		if strings.TrimSpace(payload.Input) == "" {
			return errors.New("turn_started input is empty")
		}
	case KindResponseItem:
		_, err := DecodeResponseItem(item)
		return err
	case KindCompaction:
		payload, err := DecodePayload[Compaction](item)
		if err != nil {
			return err
		}
		if strings.TrimSpace(payload.Summary) == "" || len(payload.ReplacementHistory) == 0 || payload.CoveredThroughSequence <= 0 || strings.TrimSpace(payload.SourceHash) == "" {
			return errors.New("compaction payload is incomplete")
		}
	case KindTurnCompleted:
		payload, err := DecodePayload[TurnCompleted](item)
		if err != nil {
			return err
		}
		if payload.Status != TurnStatusCompleted && payload.Status != TurnStatusFailed {
			return fmt.Errorf("turn_completed status %q is invalid", payload.Status)
		}
		if payload.Status == TurnStatusFailed && strings.TrimSpace(payload.Error) == "" {
			return errors.New("failed turn_completed payload requires an error")
		}
	case KindTurnAborted:
		payload, err := DecodePayload[TurnAborted](item)
		if err != nil {
			return err
		}
		if strings.TrimSpace(payload.Reason) == "" {
			return errors.New("turn_aborted reason is empty")
		}
	case KindTurnItemCompleted:
		payload, err := DecodePayload[TurnItemCompleted](item)
		if err != nil {
			return err
		}
		if strings.TrimSpace(payload.ID) == "" || strings.TrimSpace(payload.Kind) == "" {
			return errors.New("turn_item_completed identity is incomplete")
		}
		switch payload.Status {
		case "completed", "failed", "declined":
		default:
			return fmt.Errorf("turn_item_completed status %q is invalid", payload.Status)
		}
		if payload.CreatedAt.IsZero() || payload.CompletedAt.IsZero() {
			return errors.New("turn_item_completed timestamps are incomplete")
		}
	case KindTokenUsage:
		payload, err := DecodePayload[TokenUsage](item)
		if err != nil {
			return err
		}
		if payload.TotalTokens < 0 {
			return errors.New("token_usage total_tokens is negative")
		}
	case KindContextUpdate:
		payload, err := DecodePayload[ContextUpdate](item)
		if err != nil {
			return err
		}
		if strings.TrimSpace(payload.Title) == "" && payload.Archived == nil && strings.TrimSpace(payload.Key) == "" {
			return errors.New("context_update payload is empty")
		}
		if strings.TrimSpace(payload.Key) != "" && payload.Content == "" {
			return errors.New("dynamic context_update content is empty")
		}
	}
	return nil
}

func validateResponseItem(payload ResponseItem) error {
	switch payload.Type {
	case ResponseUserMessage:
		if strings.TrimSpace(payload.Content) == "" {
			return errors.New("user response item content is empty")
		}
	case ResponseAssistantMessage:
		if strings.TrimSpace(payload.Content) == "" && strings.TrimSpace(payload.Reasoning) == "" {
			return errors.New("assistant response item is empty")
		}
	case ResponseToolCall:
		if strings.TrimSpace(payload.CallID) == "" || strings.TrimSpace(payload.Name) == "" {
			return errors.New("tool call response item identity is incomplete")
		}
		if len(payload.Arguments) == 0 || !json.Valid(payload.Arguments) {
			return errors.New("tool call response item arguments are invalid")
		}
	case ResponseToolResult:
		if strings.TrimSpace(payload.CallID) == "" || strings.TrimSpace(payload.Name) == "" || strings.TrimSpace(payload.Status) == "" || payload.Result == nil {
			return errors.New("tool result response item is incomplete")
		}
	default:
		return fmt.Errorf("response item type %q is invalid", payload.Type)
	}
	return nil
}

func validateKind(kind Kind) error {
	value := strings.TrimSpace(string(kind))
	if value == "" || value != string(kind) || strings.ContainsAny(value, " \t\r\n") {
		return errors.New("rollout kind is empty or contains whitespace")
	}
	return nil
}

func validateID(name, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed != value || strings.ContainsAny(value, " \t\r\n") {
		return fmt.Errorf("%s ID is empty or contains whitespace", name)
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return fmt.Errorf("%s ID contains an unsafe character", name)
	}
	return nil
}

func requiresTurn(kind Kind) bool {
	switch kind {
	case KindTurnContext, KindTurnStarted, KindResponseItem, KindTurnItemCompleted,
		KindPlanUpdate, KindCompaction, KindTokenUsage, KindTurnCompleted, KindTurnAborted:
		return true
	default:
		return false
	}
}
