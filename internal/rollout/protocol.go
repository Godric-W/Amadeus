package rollout

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
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

type TurnTerminalStatus string

const (
	TurnStatusCompleted TurnTerminalStatus = "completed"
	TurnStatusFailed    TurnTerminalStatus = "failed"
)

type TurnCompleted struct {
	Status TurnTerminalStatus `json:"status"`
	Error  string             `json:"error,omitempty"`
}

type TurnAborted struct {
	Reason string `json:"reason"`
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
