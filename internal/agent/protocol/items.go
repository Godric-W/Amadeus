package protocol

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
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
	ID          string           `json:"id"`
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
	if strings.TrimSpace(item.ID) == "" {
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

type ItemStarted struct{ Item TurnItem }

func (ItemStarted) isEventMessage() {}

type ItemCompleted struct{ Item TurnItem }

func (ItemCompleted) isEventMessage() {}

type AssistantMessageDelta struct {
	ItemID string
	Delta  string
	Reset  bool
}

func (AssistantMessageDelta) isEventMessage() {}

type ReasoningDelta struct {
	ItemID string
	Delta  string
	Reset  bool
}

func (ReasoningDelta) isEventMessage() {}

type CommandOutputDelta struct {
	ItemID string
	Delta  string
}

func (CommandOutputDelta) isEventMessage() {}

type PlanItem struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

type PlanUpdated struct {
	ItemID      string
	Explanation string
	Items       []PlanItem
	Revision    int64
	UpdatedAt   time.Time
}

func (PlanUpdated) isEventMessage() {}

type ThreadTokenUsageUpdated struct {
	Usage                llm.Usage
	EstimatedInputTokens int64
	ContextWindow        int64
}

func (ThreadTokenUsageUpdated) isEventMessage() {}

type ContextCompacted struct {
	ItemID string
}

func (ContextCompacted) isEventMessage() {}

type TranscriptState struct {
	ThreadID rollout.ThreadID
	TurnID   rollout.TurnID
	Working  bool
	Items    []TurnItem
	Active   map[string]TurnItem
	Plan     *PlanUpdated
	Usage    llm.Usage
	Pending  *InteractiveRequest
	Warning  string
	Error    string
}

func NewTranscriptState(threadID rollout.ThreadID) *TranscriptState {
	return &TranscriptState{ThreadID: threadID, Active: make(map[string]TurnItem)}
}

// Apply is deterministic and idempotent for completed items. Replay can
// submit ItemCompleted without a preceding ItemStarted.
func (state *TranscriptState) Apply(event SessionEvent) error {
	if state == nil {
		return errors.New("transcript state is nil")
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if state.ThreadID == "" {
		state.ThreadID = event.ThreadID
	}
	if event.ThreadID != state.ThreadID {
		return fmt.Errorf("event thread ID %q does not match %q", event.ThreadID, state.ThreadID)
	}
	if event.TurnID != "" {
		state.TurnID = event.TurnID
	}
	if state.Active == nil {
		state.Active = make(map[string]TurnItem)
	}
	switch message := event.Message.(type) {
	case ThreadConfigured:
	case TurnStarted:
		state.Working = true
		state.Error = ""
	case TurnCompleted, TurnAborted:
		state.Working = false
		state.Pending = nil
	case ItemStarted:
		if err := message.Item.Validate(); err != nil {
			return err
		}
		state.Active[message.Item.ID] = message.Item
	case ItemCompleted:
		if err := message.Item.Validate(); err != nil {
			return err
		}
		delete(state.Active, message.Item.ID)
		replaceTranscriptItem(&state.Items, message.Item)
	case AssistantMessageDelta:
		return state.applyDelta(message.ItemID, message.Delta, message.Reset)
	case ReasoningDelta:
		return state.applyDelta(message.ItemID, message.Delta, message.Reset)
	case CommandOutputDelta:
		return state.applyDelta(message.ItemID, message.Delta, false)
	case PlanUpdated:
		copy := message
		state.Plan = &copy
	case ThreadTokenUsageUpdated:
		state.Usage = message.Usage
	case ContextCompacted:
		now := time.Now().UTC()
		replaceTranscriptItem(&state.Items, TurnItem{ID: message.ItemID, Kind: ItemContextCompaction, Status: ItemStatusCompleted, CreatedAt: now, CompletedAt: now})
	case Warning:
		state.Warning = message.Message
	case StreamError:
		if !message.WillRetry {
			state.Error = message.Message
		}
	}
	return nil
}

func (state *TranscriptState) applyDelta(itemID, delta string, reset bool) error {
	if strings.TrimSpace(itemID) == "" {
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
