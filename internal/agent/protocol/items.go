package protocol

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type ItemKind string

const (
	ItemUserMessage       ItemKind = "user_message"
	ItemAssistantMessage  ItemKind = "assistant_message"
	ItemReasoning         ItemKind = "reasoning"
	ItemToolCall          ItemKind = "tool_call"
	ItemCommandExecution  ItemKind = "command_execution"
	ItemFileChange        ItemKind = "file_change"
	ItemPlan              ItemKind = "plan"
	ItemContextCompaction ItemKind = "context_compaction"
)

func (kind ItemKind) Valid() bool {
	switch kind {
	case ItemUserMessage, ItemAssistantMessage, ItemReasoning, ItemToolCall,
		ItemCommandExecution, ItemFileChange, ItemPlan, ItemContextCompaction:
		return true
	default:
		return false
	}
}

type ItemStatus string

const (
	ItemInProgress      ItemStatus = "in_progress"
	ItemStatusCompleted ItemStatus = "completed"
	ItemFailed          ItemStatus = "failed"
	ItemDeclined        ItemStatus = "declined"
)

func (status ItemStatus) Valid() bool {
	return status == ItemInProgress || status == ItemStatusCompleted || status == ItemFailed || status == ItemDeclined
}

// TurnItem is the stable replay unit shared by live and resumed sessions.
// A terminal item must contain all facts needed to render it independently.
type TurnItem struct {
	ID          ItemID           `json:"id"`
	Kind        ItemKind         `json:"kind"`
	Status      ItemStatus       `json:"status"`
	CreatedAt   time.Time        `json:"created_at"`
	CompletedAt time.Time        `json:"completed_at,omitempty"`
	Text        string           `json:"text,omitempty"`
	ToolName    string           `json:"tool_name,omitempty"`
	CallID      string           `json:"call_id,omitempty"`
	ToolResult  *tool.ToolResult `json:"tool_result,omitempty"`
	Payload     any              `json:"payload,omitempty"`
}

func (item TurnItem) Validate() error {
	if strings.TrimSpace(string(item.ID)) == "" {
		return errors.New("turn item ID is empty")
	}
	if !item.Kind.Valid() {
		return fmt.Errorf("turn item kind %q is invalid", item.Kind)
	}
	if !item.Status.Valid() {
		return fmt.Errorf("turn item status %q is invalid", item.Status)
	}
	if item.CreatedAt.IsZero() {
		return errors.New("turn item created time is zero")
	}
	if item.Status != ItemInProgress && item.CompletedAt.IsZero() {
		return errors.New("terminal turn item has no completion time")
	}
	return nil
}

type ItemStartedEvent struct {
	ThreadID ThreadID
	TurnID   TurnID
	Item     TurnItem
}

func (ItemStartedEvent) isEventMsg() {}

type ItemCompletedEvent struct {
	ThreadID ThreadID
	TurnID   TurnID
	Item     TurnItem
}

func (ItemCompletedEvent) isEventMsg() {}

type AgentMessageContentDeltaEvent struct {
	ThreadID ThreadID
	TurnID   TurnID
	ItemID   ItemID
	Delta    string
	Reset    bool
}

func (AgentMessageContentDeltaEvent) isEventMsg() {}

type ReasoningContentDeltaEvent struct {
	ThreadID ThreadID
	TurnID   TurnID
	ItemID   ItemID
	Delta    string
	Reset    bool
}

func (ReasoningContentDeltaEvent) isEventMsg() {}

type CommandOutputDeltaEvent struct {
	ThreadID ThreadID
	TurnID   TurnID
	ItemID   ItemID
	Delta    string
}

func (CommandOutputDeltaEvent) isEventMsg() {}

type PlanItem struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

type PlanUpdateEvent struct {
	ThreadID    ThreadID
	TurnID      TurnID
	ItemID      ItemID
	Explanation string
	Items       []PlanItem
	Revision    int64
	UpdatedAt   time.Time
}

func (PlanUpdateEvent) isEventMsg() {}

type TokenCountEvent struct {
	ThreadID             ThreadID
	TurnID               TurnID
	Usage                llm.Usage
	EstimatedInputTokens int64
	ContextWindow        int64
}

func (TokenCountEvent) isEventMsg() {}

type ContextCompactedEvent struct {
	ThreadID ThreadID
	TurnID   TurnID
	ItemID   ItemID
}

func (ContextCompactedEvent) isEventMsg() {}

type TranscriptState struct {
	ThreadID ThreadID
	TurnID   TurnID
	Working  bool
	Items    []TurnItem
	Active   map[ItemID]TurnItem
	Plan     *PlanUpdateEvent
	Usage    llm.Usage
	Pending  *ApprovalRequestEvent
	Warning  string
	Error    string
}

func NewTranscriptState(threadID ThreadID) *TranscriptState {
	return &TranscriptState{ThreadID: threadID, Active: make(map[ItemID]TurnItem)}
}

// Apply is deterministic and idempotent for completed items. Replay can
// submit ItemCompleted without a preceding ItemStarted.
func (state *TranscriptState) Apply(event Event) error {
	if state == nil {
		return errors.New("transcript state is nil")
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if state.ThreadID == "" {
		state.ThreadID = ThreadIDOf(event.Msg)
	}
	threadID := ThreadIDOf(event.Msg)
	if threadID != state.ThreadID {
		return fmt.Errorf("event thread ID %q does not match %q", threadID, state.ThreadID)
	}
	if turnID := TurnIDOf(event.Msg); turnID != "" {
		state.TurnID = turnID
	}
	if state.Active == nil {
		state.Active = make(map[ItemID]TurnItem)
	}
	switch message := event.Msg.(type) {
	case SessionConfiguredEvent:
	case TurnStartedEvent:
		state.Working = true
		state.Error = ""
	case TurnCompleteEvent, TurnAbortedEvent:
		state.Working = false
		state.Pending = nil
	case ItemStartedEvent:
		if err := message.Item.Validate(); err != nil {
			return err
		}
		state.Active[message.Item.ID] = message.Item
	case ItemCompletedEvent:
		if err := message.Item.Validate(); err != nil {
			return err
		}
		delete(state.Active, message.Item.ID)
		replaceTranscriptItem(&state.Items, message.Item)
	case AgentMessageContentDeltaEvent:
		return state.applyDelta(message.ItemID, message.Delta, message.Reset)
	case ReasoningContentDeltaEvent:
		return state.applyDelta(message.ItemID, message.Delta, message.Reset)
	case CommandOutputDeltaEvent:
		return state.applyDelta(message.ItemID, message.Delta, false)
	case PlanUpdateEvent:
		copy := message
		state.Plan = &copy
	case TokenCountEvent:
		state.Usage = message.Usage
	case ContextCompactedEvent:
		now := time.Now().UTC()
		replaceTranscriptItem(&state.Items, TurnItem{ID: message.ItemID, Kind: ItemContextCompaction, Status: ItemStatusCompleted, CreatedAt: now, CompletedAt: now})
	case WarningEvent:
		state.Warning = message.Message
	case StreamErrorEvent:
		if !message.WillRetry {
			state.Error = message.Message
		}
	}
	return nil
}

func ScopeItemEventMsg(message EventMsg, threadID ThreadID, turnID TurnID) EventMsg {
	switch value := message.(type) {
	case ItemStartedEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case ItemCompletedEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case AgentMessageContentDeltaEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case ReasoningContentDeltaEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case CommandOutputDeltaEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case PlanUpdateEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case TokenCountEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case ContextCompactedEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	default:
		return message
	}
}

func ItemEventThreadID(message EventMsg) ThreadID {
	switch value := message.(type) {
	case ItemStartedEvent:
		return value.ThreadID
	case ItemCompletedEvent:
		return value.ThreadID
	case AgentMessageContentDeltaEvent:
		return value.ThreadID
	case ReasoningContentDeltaEvent:
		return value.ThreadID
	case CommandOutputDeltaEvent:
		return value.ThreadID
	case PlanUpdateEvent:
		return value.ThreadID
	case TokenCountEvent:
		return value.ThreadID
	case ContextCompactedEvent:
		return value.ThreadID
	default:
		return ""
	}
}

func ItemEventTurnID(message EventMsg) TurnID {
	switch value := message.(type) {
	case ItemStartedEvent:
		return value.TurnID
	case ItemCompletedEvent:
		return value.TurnID
	case AgentMessageContentDeltaEvent:
		return value.TurnID
	case ReasoningContentDeltaEvent:
		return value.TurnID
	case CommandOutputDeltaEvent:
		return value.TurnID
	case PlanUpdateEvent:
		return value.TurnID
	case TokenCountEvent:
		return value.TurnID
	case ContextCompactedEvent:
		return value.TurnID
	default:
		return ""
	}
}

func (state *TranscriptState) applyDelta(itemID ItemID, delta string, reset bool) error {
	if strings.TrimSpace(string(itemID)) == "" {
		return errors.New("transcript delta item ID is empty")
	}
	item, ok := state.Active[itemID]
	if !ok {
		for _, completed := range state.Items {
			if completed.ID == itemID {
				return nil
			}
		}
		return fmt.Errorf("transcript delta references unknown item %q", itemID)
	}
	if reset {
		item.Text = delta
	} else {
		item.Text += delta
	}
	state.Active[itemID] = item
	return nil
}

func replaceTranscriptItem(items *[]TurnItem, item TurnItem) {
	for index := range *items {
		if (*items)[index].ID == item.ID {
			(*items)[index] = item
			return
		}
	}
	*items = append(*items, item)
}
