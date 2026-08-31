package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/tool"
)

// TurnItemPayload is the typed payload boundary for a stable TurnItem.
// Implementations are decoded by ItemKind; callers must not use a generic map
// as a canonical payload.
type TurnItemPayload interface{ isTurnItemPayload() }

type ToolCallItemPayload struct {
	ActionSummary string `json:"action_summary,omitempty"`
	Detail        string `json:"detail,omitempty"`
	SideEffect    string `json:"side_effect,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	Partial       bool   `json:"partial,omitempty"`
}

func (ToolCallItemPayload) isTurnItemPayload() {}

type CommandExecutionItemPayload struct {
	ActionSummary string `json:"action_summary,omitempty"`
	Detail        string `json:"detail,omitempty"`
	SideEffect    string `json:"side_effect,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	Partial       bool   `json:"partial,omitempty"`
}

func (CommandExecutionItemPayload) isTurnItemPayload() {}

type FileChangeItemPayload struct {
	ActionSummary string `json:"action_summary,omitempty"`
	Detail        string `json:"detail,omitempty"`
	SideEffect    string `json:"side_effect,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	Partial       bool   `json:"partial,omitempty"`
}

func (FileChangeItemPayload) isTurnItemPayload() {}

func validateTurnItemPayload(kind ItemKind, payload TurnItemPayload) error {
	if payload == nil {
		switch kind {
		case ItemToolCall, ItemCommandExecution, ItemFileChange, ItemContextCompaction:
			return fmt.Errorf("%s item payload is missing", kind)
		}
		return nil
	}
	validateActivity := func(sideEffect string, durationMS int64) error {
		if durationMS < 0 {
			return errors.New("turn item payload duration is negative")
		}
		if sideEffect != "" {
			valid := sideEffect == "none" || sideEffect == "read" || sideEffect == "write" || sideEffect == "execute" || sideEffect == "network"
			if !valid {
				return fmt.Errorf("turn item payload side effect %q is invalid", sideEffect)
			}
		}
		return nil
	}
	switch kind {
	case ItemToolCall:
		value, ok := payload.(ToolCallItemPayload)
		if !ok {
			return errors.New("tool call item payload type is invalid")
		}
		return validateActivity(value.SideEffect, value.DurationMS)
	case ItemCommandExecution:
		value, ok := payload.(CommandExecutionItemPayload)
		if !ok {
			return errors.New("command execution item payload type is invalid")
		}
		return validateActivity(value.SideEffect, value.DurationMS)
	case ItemFileChange:
		value, ok := payload.(FileChangeItemPayload)
		if !ok {
			return errors.New("file change item payload type is invalid")
		}
		return validateActivity(value.SideEffect, value.DurationMS)
	case ItemContextCompaction:
		value, ok := payload.(ContextCompactionItem)
		if !ok || !value.Validate() {
			return errors.New("context compaction item payload is invalid")
		}
	case ItemUserMessage, ItemAssistantMessage, ItemReasoning, ItemPlan, ItemCollabAgentToolCall:
		return fmt.Errorf("turn item kind %q does not accept a payload", kind)
	default:
		return fmt.Errorf("turn item kind %q is invalid", kind)
	}
	return nil
}

type turnItemWire struct {
	ID                  ItemID                   `json:"id"`
	Kind                ItemKind                 `json:"kind"`
	Status              ItemStatus               `json:"status"`
	CreatedAt           time.Time                `json:"created_at"`
	CompletedAt         time.Time                `json:"completed_at,omitempty"`
	Text                string                   `json:"text,omitempty"`
	ClientUserMessageID string                   `json:"client_user_message_id,omitempty"`
	ToolName            string                   `json:"tool_name,omitempty"`
	CallID              string                   `json:"call_id,omitempty"`
	ToolResult          *tool.ToolResult         `json:"tool_result,omitempty"`
	CollabAgent         *CollabAgentToolCallItem `json:"collab_agent,omitempty"`
	Payload             json.RawMessage          `json:"payload,omitempty"`
}

func (item TurnItem) MarshalJSON() ([]byte, error) {
	if err := item.Validate(); err != nil {
		return nil, err
	}
	var payload json.RawMessage
	if item.Payload != nil {
		encoded, err := json.Marshal(item.Payload)
		if err != nil {
			return nil, fmt.Errorf("encode turn item payload: %w", err)
		}
		payload = encoded
	}
	return json.Marshal(turnItemWire{
		ID: item.ID, Kind: item.Kind, Status: item.Status, CreatedAt: item.CreatedAt, CompletedAt: item.CompletedAt,
		Text: item.Text, ClientUserMessageID: item.ClientUserMessageID, ToolName: item.ToolName, CallID: item.CallID,
		ToolResult: item.ToolResult, CollabAgent: item.CollabAgent, Payload: payload,
	})
}

func (item *TurnItem) UnmarshalJSON(content []byte) error {
	if item == nil {
		return errors.New("turn item target is nil")
	}
	var wire turnItemWire
	if err := json.Unmarshal(content, &wire); err != nil {
		return fmt.Errorf("decode turn item: %w", err)
	}
	payload, err := decodeTurnItemPayload(wire.Kind, wire.Payload)
	if err != nil {
		return err
	}
	decoded := TurnItem{
		ID: wire.ID, Kind: wire.Kind, Status: wire.Status, CreatedAt: wire.CreatedAt, CompletedAt: wire.CompletedAt,
		Text: wire.Text, ClientUserMessageID: wire.ClientUserMessageID, ToolName: wire.ToolName, CallID: wire.CallID,
		ToolResult: wire.ToolResult, CollabAgent: wire.CollabAgent, Payload: payload,
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*item = decoded
	return nil
}

func decodeTurnItemPayload(kind ItemKind, raw json.RawMessage) (TurnItemPayload, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	switch kind {
	case ItemToolCall:
		var value ToolCallItemPayload
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("decode %s item payload: %w", kind, err)
		}
		return value, nil
	case ItemCommandExecution:
		var value CommandExecutionItemPayload
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("decode %s item payload: %w", kind, err)
		}
		return value, nil
	case ItemFileChange:
		var value FileChangeItemPayload
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("decode %s item payload: %w", kind, err)
		}
		return value, nil
	case ItemContextCompaction:
		var value ContextCompactionItem
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("decode %s item payload: %w", kind, err)
		}
		return value, nil
	case ItemUserMessage, ItemAssistantMessage, ItemReasoning, ItemPlan, ItemCollabAgentToolCall:
		return nil, fmt.Errorf("turn item kind %q does not accept a payload", kind)
	default:
		return nil, fmt.Errorf("turn item kind %q is invalid", kind)
	}
}
