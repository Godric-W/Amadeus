package rollout

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type RolloutItem interface {
	isRolloutItem()
	Validate() error
}

type SessionMetaItem struct {
	ThreadID      protocol.ThreadID `json:"thread_id"`
	CWD           string            `json:"cwd"`
	Title         string            `json:"title"`
	ModelProvider string            `json:"model_provider,omitempty"`
	Model         string            `json:"model,omitempty"`
	GitSHA        string            `json:"git_sha,omitempty"`
	GitBranch     string            `json:"git_branch,omitempty"`
	GitOriginURL  string            `json:"git_origin_url,omitempty"`
	Archived      bool              `json:"archived,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
}

func (SessionMetaItem) isRolloutItem() {}

func (item SessionMetaItem) Validate() error {
	if err := validateID("thread", string(item.ThreadID)); err != nil {
		return err
	}
	if strings.TrimSpace(item.CWD) == "" || strings.TrimSpace(item.Title) == "" || item.CreatedAt.IsZero() {
		return errors.New("session meta item is incomplete")
	}
	return nil
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
	ThreadID protocol.ThreadID `json:"thread_id"`
	TurnID   protocol.TurnID   `json:"turn_id"`

	Type      ResponseItemType   `json:"response_type"`
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

func (ResponseItem) isRolloutItem() {}

func NewResponseItem(item ResponseItem) (ResponseItem, error) {
	if err := validateResponseItem(item, false); err != nil {
		return ResponseItem{}, err
	}
	return item, nil
}

func (item ResponseItem) Validate() error { return validateResponseItem(item, true) }

func validateResponseItem(item ResponseItem, requireScope bool) error {
	if requireScope {
		if err := validateID("thread", string(item.ThreadID)); err != nil {
			return err
		}
		if err := validateID("turn", string(item.TurnID)); err != nil {
			return err
		}
	}
	switch item.Type {
	case ResponseUserMessage:
		if strings.TrimSpace(item.Content) == "" {
			return errors.New("user response item content is empty")
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

type ReplacementMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompactedItem struct {
	ThreadID               protocol.ThreadID    `json:"thread_id"`
	TurnID                 protocol.TurnID      `json:"turn_id"`
	Summary                string               `json:"summary"`
	ReplacementHistory     []ReplacementMessage `json:"replacement_history"`
	CoveredThroughSequence int64                `json:"covered_through_sequence"`
	SourceHash             string               `json:"source_hash"`
	Provider               string               `json:"provider,omitempty"`
	Model                  string               `json:"model,omitempty"`
}

func (CompactedItem) isRolloutItem() {}

func (item CompactedItem) Validate() error {
	if err := validateID("thread", string(item.ThreadID)); err != nil {
		return err
	}
	if err := validateID("turn", string(item.TurnID)); err != nil {
		return err
	}
	if strings.TrimSpace(item.Summary) == "" || len(item.ReplacementHistory) == 0 || item.CoveredThroughSequence <= 0 || strings.TrimSpace(item.SourceHash) == "" {
		return errors.New("compacted item is incomplete")
	}
	return nil
}

type TurnContextItem struct {
	ThreadID protocol.ThreadID `json:"thread_id"`
	TurnID   protocol.TurnID   `json:"turn_id"`
	Provider string            `json:"provider"`
	Model    string            `json:"model"`
	CWD      string            `json:"cwd"`
	Shell    string            `json:"shell,omitempty"`

	CurrentDate string `json:"current_date,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	Mode        string `json:"mode"`
	Personality string `json:"personality,omitempty"`

	OutputSchema       json.RawMessage `json:"output_schema,omitempty"`
	OutputSchemaStrict bool            `json:"output_schema_strict,omitempty"`
}

func (TurnContextItem) isRolloutItem() {}

func (item TurnContextItem) Validate() error {
	if err := validateID("thread", string(item.ThreadID)); err != nil {
		return err
	}
	if err := validateID("turn", string(item.TurnID)); err != nil {
		return err
	}
	if strings.TrimSpace(item.Provider) == "" || strings.TrimSpace(item.Model) == "" || strings.TrimSpace(item.CWD) == "" || strings.TrimSpace(item.Mode) == "" {
		return errors.New("turn context item is incomplete")
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
	if err := validateID("thread", string(protocol.ThreadIDOf(item.Msg))); err != nil {
		return err
	}
	return nil
}

func ScopeItem(item RolloutItem, threadID protocol.ThreadID, turnID protocol.TurnID) RolloutItem {
	switch value := item.(type) {
	case SessionMetaItem:
		value.ThreadID = threadID
		return value
	case ResponseItem:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case CompactedItem:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case TurnContextItem:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case EventMsgItem:
		value.Msg = protocol.ScopeEventMsg(value.Msg, threadID, turnID)
		return value
	default:
		return item
	}
}

func ThreadIDOf(item RolloutItem) protocol.ThreadID {
	switch value := item.(type) {
	case SessionMetaItem:
		return value.ThreadID
	case ResponseItem:
		return value.ThreadID
	case CompactedItem:
		return value.ThreadID
	case TurnContextItem:
		return value.ThreadID
	case EventMsgItem:
		return protocol.ThreadIDOf(value.Msg)
	default:
		return ""
	}
}

func TurnIDOf(item RolloutItem) protocol.TurnID {
	switch value := item.(type) {
	case ResponseItem:
		return value.TurnID
	case CompactedItem:
		return value.TurnID
	case TurnContextItem:
		return value.TurnID
	case EventMsgItem:
		return protocol.TurnIDOf(value.Msg)
	default:
		return ""
	}
}

func CloneItem(item RolloutItem) RolloutItem {
	switch value := item.(type) {
	case SessionMetaItem:
		return value
	case ResponseItem:
		value.Arguments = append(json.RawMessage(nil), value.Arguments...)
		value.Metadata = cloneMap(value.Metadata)
		value.Parts = append([]tool.ContentPart(nil), value.Parts...)
		if value.Result != nil {
			result := value.Result.Clone()
			value.Result = &result
		}
		if value.Error != nil {
			errorValue := *value.Error
			value.Error = &errorValue
		}
		return value
	case CompactedItem:
		value.ReplacementHistory = append([]ReplacementMessage(nil), value.ReplacementHistory...)
		return value
	case TurnContextItem:
		value.OutputSchema = append(json.RawMessage(nil), value.OutputSchema...)
		return value
	case EventMsgItem:
		encoded, err := protocol.EncodeEventMsg(value.Msg)
		if err != nil {
			return value
		}
		cloned, err := protocol.DecodeEventMsg(encoded)
		if err != nil {
			return value
		}
		return EventMsgItem{Msg: cloned}
	default:
		return item
	}
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		return nil
	}
	var result map[string]any
	if json.Unmarshal(encoded, &result) != nil {
		return nil
	}
	return result
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
