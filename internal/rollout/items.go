package rollout

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type RolloutItem interface {
	isRolloutItem()
	Validate() error
}

type SessionMetaItem struct {
	SessionID        protocol.SessionID     `json:"session_id"`
	ID               protocol.ThreadID      `json:"id"`
	ParentThreadID   *protocol.ThreadID     `json:"parent_thread_id,omitempty"`
	Source           protocol.SessionSource `json:"source"`
	CWD              string                 `json:"cwd"`
	Title            string                 `json:"title"`
	ModelProvider    string                 `json:"model_provider,omitempty"`
	Model            string                 `json:"model,omitempty"`
	BaseInstructions llm.BaseInstructions   `json:"base_instructions"`
	GitSHA           string                 `json:"git_sha,omitempty"`
	GitBranch        string                 `json:"git_branch,omitempty"`
	GitOriginURL     string                 `json:"git_origin_url,omitempty"`
	Archived         bool                   `json:"archived,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
}

func (SessionMetaItem) isRolloutItem() {}

func (item SessionMetaItem) Validate() error {
	if item.SessionID.IsZero() || item.ID.IsZero() {
		return errors.New("session meta identity is incomplete")
	}
	if strings.TrimSpace(item.CWD) == "" || strings.TrimSpace(item.Title) == "" || item.CreatedAt.IsZero() {
		return errors.New("session meta item is incomplete")
	}
	if err := item.BaseInstructions.ValidatePersisted(); err != nil {
		return fmt.Errorf("session meta base instructions: %w", err)
	}
	if err := item.Source.Validate(); err != nil {
		return fmt.Errorf("session meta source: %w", err)
	}
	if item.Source.IsSubAgent() {
		if item.ParentThreadID == nil || item.ParentThreadID.IsZero() || *item.ParentThreadID != item.Source.SubAgent.ParentThreadID {
			return errors.New("sub-agent session meta parent identity is inconsistent")
		}
		if item.ID == *item.ParentThreadID {
			return errors.New("sub-agent session meta cannot be its own parent")
		}
	} else {
		if item.ParentThreadID != nil {
			return errors.New("root session meta has parent thread ID")
		}
		if item.SessionID != protocol.SessionIDFromThreadID(item.ID) {
			return errors.New("root session meta session ID does not match thread ID")
		}
	}
	return nil
}

type ResponseItemType string

const (
	ResponseUserMessage      ResponseItemType = "user_message"
	ResponseContextMessage   ResponseItemType = "context_message"
	ResponseAssistantMessage ResponseItemType = "assistant_message"
	ResponseToolCall         ResponseItemType = "tool_call"
	ResponseToolResult       ResponseItemType = "tool_result"
)

type ContextKind string

const (
	ContextKindWorldState    ContextKind = "world_state"
	ContextKindExplicitSkill ContextKind = "explicit_skill"
	ContextKindTurnBudget    ContextKind = "turn_budget"
	ContextKindGoal          ContextKind = "goal"
)

func (kind ContextKind) Valid() bool {
	return kind == ContextKindWorldState || kind == ContextKindExplicitSkill || kind == ContextKindTurnBudget || kind == ContextKindGoal
}

type ResponseError struct {
	Kind    string `json:"kind,omitempty"`
	Message string `json:"message"`
}

type ResponseItem struct {
	ThreadID protocol.ThreadID `json:"thread_id"`
	TurnID   protocol.TurnID   `json:"turn_id"`

	Type        ResponseItemType   `json:"response_type"`
	Role        string             `json:"role,omitempty"`
	ContextKind ContextKind        `json:"context_kind,omitempty"`
	Content     string             `json:"content,omitempty"`
	Reasoning   string             `json:"reasoning_content,omitempty"`
	CallID      string             `json:"call_id,omitempty"`
	Name        string             `json:"name,omitempty"`
	Arguments   json.RawMessage    `json:"arguments,omitempty"`
	Status      string             `json:"status,omitempty"`
	Result      *tool.ToolResult   `json:"result,omitempty"`
	Error       *ResponseError     `json:"error,omitempty"`
	Metadata    map[string]any     `json:"metadata,omitempty"`
	Partial     bool               `json:"partial,omitempty"`
	Duration    int64              `json:"duration_nanos,omitempty"`
	Parts       []tool.ContentPart `json:"parts,omitempty"`
}

func (ResponseItem) isRolloutItem() {}

func NewResponseItem(item ResponseItem) (ResponseItem, error) {
	if err := validateResponseItem(item, false); err != nil {
		return ResponseItem{}, err
	}
	return item, nil
}

func (item ResponseItem) Validate() error { return validateResponseItem(item, true) }

func (item ResponseItem) ValidateUnscoped() error { return validateResponseItem(item, false) }

func validateResponseItem(item ResponseItem, requireScope bool) error {
	if requireScope {
		if item.ThreadID.IsZero() {
			return errors.New("response item thread ID is empty")
		}
		if err := validateID("turn", string(item.TurnID)); err != nil {
			return err
		}
	}
	switch item.Type {
	case ResponseUserMessage:
		if item.Content == "" {
			return errors.New("user response item content is empty")
		}
	case ResponseContextMessage:
		if !item.ContextKind.Valid() {
			return errors.New("context response item kind is invalid")
		}
		if item.Role != string(llm.RoleDeveloper) && item.Role != string(llm.RoleUser) {
			return errors.New("context response item role is invalid")
		}
		if strings.TrimSpace(item.Content) == "" {
			return errors.New("context response item content is empty")
		}
	case ResponseAssistantMessage:
		if strings.TrimSpace(item.Content) == "" && strings.TrimSpace(item.Reasoning) == "" {
			return errors.New("assistant response item is empty")
		}
	case ResponseToolCall:
		if strings.TrimSpace(item.CallID) == "" || strings.TrimSpace(item.Name) == "" {
			return errors.New("tool call response item identity is incomplete")
		}
		if len(item.Arguments) == 0 || !json.Valid(item.Arguments) {
			return errors.New("tool call response item arguments are invalid")
		}
	case ResponseToolResult:
		if strings.TrimSpace(item.CallID) == "" || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Status) == "" || item.Result == nil {
			return errors.New("tool result response item is incomplete")
		}
	default:
		return fmt.Errorf("response item type %q is invalid", item.Type)
	}
	return nil
}

func NewContextResponseItem(message llm.ResponseItem, kind ContextKind) (ResponseItem, error) {
	item := ResponseItem{Type: ResponseContextMessage, Role: string(message.Role), ContextKind: kind, Content: message.Content}
	if err := validateResponseItem(item, false); err != nil {
		return ResponseItem{}, err
	}
	return item, nil
}

type WorldStateItem struct {
	ThreadID protocol.ThreadID          `json:"thread_id"`
	TurnID   protocol.TurnID            `json:"turn_id,omitempty"`
	Full     bool                       `json:"full,omitempty"`
	Sections map[string]json.RawMessage `json:"sections"`
}

func (WorldStateItem) isRolloutItem() {}

func (item WorldStateItem) Validate() error {
	if item.ThreadID.IsZero() {
		return errors.New("world state item thread ID is empty")
	}
	if len(item.Sections) == 0 {
		return errors.New("world state item has no sections")
	}
	for id, snapshot := range item.Sections {
		if strings.TrimSpace(id) == "" || len(snapshot) == 0 || !json.Valid(snapshot) {
			return errors.New("world state item section is invalid")
		}
	}
	return nil
}

type CompactedItem struct {
	ThreadID               protocol.ThreadID          `json:"thread_id"`
	TurnID                 protocol.TurnID            `json:"turn_id"`
	Trigger                protocol.CompactionTrigger `json:"trigger"`
	Reason                 protocol.CompactionReason  `json:"reason"`
	Phase                  protocol.CompactionPhase   `json:"phase"`
	Summary                string                     `json:"summary"`
	ReplacementHistory     []llm.ResponseItem         `json:"replacement_history"`
	ReplacementOrigins     []ReplacementOrigin        `json:"replacement_origins"`
	CoveredThroughSequence int64                      `json:"covered_through_sequence"`
	SourceHash             string                     `json:"source_hash"`
	ResetWorldState        bool                       `json:"reset_world_state,omitempty"`
	ResetTurnContext       bool                       `json:"reset_turn_context,omitempty"`
	Provider               string                     `json:"provider,omitempty"`
	Model                  string                     `json:"model,omitempty"`
}

type ReplacementOrigin string

const (
	ReplacementOriginUser       ReplacementOrigin = "user"
	ReplacementOriginRuntime    ReplacementOrigin = "runtime"
	ReplacementOriginCompaction ReplacementOrigin = "compaction"
)

func (origin ReplacementOrigin) Valid() bool {
	return origin == ReplacementOriginUser || origin == ReplacementOriginRuntime || origin == ReplacementOriginCompaction
}

func (CompactedItem) isRolloutItem() {}

func (item CompactedItem) Validate() error {
	if item.ThreadID.IsZero() {
		return errors.New("compacted item thread ID is empty")
	}
	if err := validateID("turn", string(item.TurnID)); err != nil {
		return err
	}
	if !item.Trigger.Valid() || !item.Reason.Valid() || !item.Phase.Valid() {
		return errors.New("compacted item lifecycle is invalid")
	}
	if strings.TrimSpace(item.Summary) == "" || len(item.ReplacementHistory) == 0 || len(item.ReplacementOrigins) != len(item.ReplacementHistory) || item.CoveredThroughSequence <= 0 || strings.TrimSpace(item.SourceHash) == "" {
		return errors.New("compacted item is incomplete")
	}
	for index, replacement := range item.ReplacementHistory {
		origin := item.ReplacementOrigins[index]
		if !origin.Valid() || (replacement.Role != llm.RoleUser && replacement.Role != llm.RoleDeveloper) || strings.TrimSpace(replacement.Content) == "" || len(replacement.ToolCalls) > 0 || replacement.ToolCallID != "" {
			return errors.New("compacted replacement history is invalid")
		}
		if origin == ReplacementOriginUser && replacement.Role != llm.RoleUser || origin == ReplacementOriginRuntime && replacement.Role != llm.RoleUser && replacement.Role != llm.RoleDeveloper || origin == ReplacementOriginCompaction && replacement.Role != llm.RoleUser {
			return errors.New("compacted replacement origin does not match role")
		}
	}
	if item.ReplacementOrigins[len(item.ReplacementOrigins)-1] != ReplacementOriginCompaction {
		return errors.New("compacted replacement history does not end with compaction summary")
	}
	return nil
}

type TurnContextItem struct {
	ThreadID        protocol.ThreadID    `json:"thread_id"`
	TurnID          protocol.TurnID      `json:"turn_id"`
	Provider        string               `json:"provider"`
	Model           string               `json:"model"`
	ReasoningEffort *llm.ReasoningEffort `json:"reasoning_effort,omitempty"`
	CWD             string               `json:"cwd"`
	Shell           string               `json:"shell,omitempty"`

	CurrentDate string `json:"current_date,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	Mode        string `json:"mode"`
	Personality string `json:"personality,omitempty"`

	OutputSchema       json.RawMessage `json:"output_schema,omitempty"`
	OutputSchemaStrict bool            `json:"output_schema_strict,omitempty"`
}

func (TurnContextItem) isRolloutItem() {}

func (item TurnContextItem) Validate() error {
	if item.ThreadID.IsZero() {
		return errors.New("turn context item thread ID is empty")
	}
	if err := validateID("turn", string(item.TurnID)); err != nil {
		return err
	}
	if strings.TrimSpace(item.Provider) == "" || strings.TrimSpace(item.Model) == "" || strings.TrimSpace(item.CWD) == "" || strings.TrimSpace(item.Mode) == "" {
		return errors.New("turn context item is incomplete")
	}
	if item.ReasoningEffort != nil && !item.ReasoningEffort.Valid() {
		return errors.New("turn context reasoning effort is invalid")
	}
	if len(item.OutputSchema) != 0 && !json.Valid(item.OutputSchema) {
		return errors.New("turn context output schema is invalid")
	}
	return nil
}

type EventMsgItem struct {
	Msg protocol.EventMsg `json:"-"`
}

func (EventMsgItem) isRolloutItem() {}

func NewEventMsgItem(message protocol.EventMsg) (EventMsgItem, error) {
	if message == nil {
		return EventMsgItem{}, errors.New("event message item is nil")
	}
	if _, err := protocol.EncodeEventMsg(message); err != nil {
		return EventMsgItem{}, err
	}
	return EventMsgItem{Msg: message}, nil
}

func (item EventMsgItem) Validate() error {
	if item.Msg == nil {
		return errors.New("event message item is nil")
	}
	if _, err := protocol.EncodeEventMsg(item.Msg); err != nil {
		return err
	}
	if protocol.ThreadIDOf(item.Msg).IsZero() {
		return errors.New("event message item thread ID is empty")
	}
	return nil
}

func validateID(name, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed != value || strings.ContainsAny(value, " \t\r\n") {
		return fmt.Errorf("%s ID is empty or contains whitespace", name)
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' || character == ':' {
			continue
		}
		return fmt.Errorf("%s ID contains an unsafe character", name)
	}
	return nil
}
