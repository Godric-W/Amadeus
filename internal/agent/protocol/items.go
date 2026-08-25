package protocol

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type ItemKind string

const (
	ItemUserMessage         ItemKind = "user_message"
	ItemAssistantMessage    ItemKind = "assistant_message"
	ItemReasoning           ItemKind = "reasoning"
	ItemToolCall            ItemKind = "tool_call"
	ItemCommandExecution    ItemKind = "command_execution"
	ItemFileChange          ItemKind = "file_change"
	ItemPlan                ItemKind = "plan"
	ItemContextCompaction   ItemKind = "context_compaction"
	ItemCollabAgentToolCall ItemKind = "collab_agent_tool_call"
)

func (kind ItemKind) Valid() bool {
	switch kind {
	case ItemUserMessage, ItemAssistantMessage, ItemReasoning, ItemToolCall,
		ItemCommandExecution, ItemFileChange, ItemPlan, ItemContextCompaction, ItemCollabAgentToolCall:
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
	Payload             any                      `json:"payload,omitempty"`
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
	if item.Kind == ItemCollabAgentToolCall {
		if item.CollabAgent == nil {
			return errors.New("collaboration turn item payload is missing")
		}
		if err := item.CollabAgent.Validate(); err != nil {
			return err
		}
		if item.CollabAgent.ID != item.ID || !item.CollabAgent.CreatedAt.Equal(item.CreatedAt) {
			return errors.New("collaboration turn item identity is inconsistent")
		}
		if item.ToolName != "" && item.ToolName != string(item.CollabAgent.Tool) {
			return errors.New("collaboration turn item tool is inconsistent")
		}
		expectedStatus := CollabAgentToolCompleted
		switch item.Status {
		case ItemInProgress:
			expectedStatus = CollabAgentToolInProgress
		case ItemFailed, ItemDeclined:
			expectedStatus = CollabAgentToolFailed
		}
		if item.CollabAgent.Status != expectedStatus {
			return errors.New("collaboration turn item status is inconsistent")
		}
		if item.Status == ItemInProgress {
			if !item.CompletedAt.IsZero() {
				return errors.New("in-progress collaboration turn item has completion time")
			}
		} else if item.CollabAgent.CompletedAt == nil || !item.CollabAgent.CompletedAt.Equal(item.CompletedAt) {
			return errors.New("collaboration turn item completion time is inconsistent")
		}
	} else if item.CollabAgent != nil {
		return errors.New("non-collaboration turn item has collaboration payload")
	}
	if item.Kind == ItemContextCompaction {
		payload, ok := item.Payload.(ContextCompactionItem)
		if !ok || !payload.Validate() {
			return errors.New("context compaction item payload is invalid")
		}
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

type TokenCountEvent struct {
	ThreadID                ThreadID
	TurnID                  TurnID
	Info                    *TokenUsageInfo
	ActiveContextTokens     int64
	ActiveContextEstimated  bool
	ObservedThroughSequence uint64
}

func (TokenCountEvent) isEventMsg() {}

func (event TokenCountEvent) Validate() error {
	if event.ActiveContextTokens < 0 {
		return errors.New("active context tokens are negative")
	}
	if event.Info == nil {
		return nil
	}
	if event.Info.TotalTokenUsage.TotalTokens > 0 && event.ObservedThroughSequence == 0 {
		return errors.New("token usage checkpoint has no observed history watermark")
	}
	return event.Info.Validate()
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
	case PlanDeltaEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case TokenCountEvent:
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
	case PlanDeltaEvent:
		return value.ThreadID
	case TokenCountEvent:
		return value.ThreadID
	default:
		return ThreadID{}
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
	case PlanDeltaEvent:
		return value.TurnID
	case TokenCountEvent:
		return value.TurnID
	default:
		return ""
	}
}
