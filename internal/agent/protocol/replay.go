package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

// NewCompletedItem converts the runtime item into its durable, self-contained
// rollout representation. The conversion is kept here so the runtime protocol
// remains the single source of item semantics while rollout stays dependency-free.
func NewCompletedItem(item TurnItem) (rollout.Item, error) {
	if err := item.Validate(); err != nil {
		return rollout.Item{}, err
	}
	payload, err := json.Marshal(item.Payload)
	if item.Payload == nil {
		payload = nil
	}
	if err != nil {
		return rollout.Item{}, fmt.Errorf("encode completed turn item payload: %w", err)
	}
	return rollout.NewItem(rollout.KindTurnItemCompleted, rollout.TurnItemCompleted{
		ID: item.ID, Kind: string(item.Kind), Status: string(item.Status),
		CreatedAt: item.CreatedAt, CompletedAt: item.CompletedAt, Text: item.Text,
		ToolName: item.ToolName, CallID: item.CallID, Payload: payload, ToolResult: cloneToolResult(item.ToolResult),
	})
}

// DecodeCompletedItem reads one completed item without replaying transient
// ItemStarted or delta events. This is the primitive used by resume projections.
func DecodeCompletedItem(line rollout.Line) (TurnItem, error) {
	if line.Item.Kind != rollout.KindTurnItemCompleted {
		return TurnItem{}, fmt.Errorf("rollout item %q is not a completed turn item", line.Item.Kind)
	}
	payload, err := rollout.DecodePayload[rollout.TurnItemCompleted](line.Item)
	if err != nil {
		return TurnItem{}, err
	}
	itemPayload := any(nil)
	if len(payload.Payload) != 0 && string(payload.Payload) != "null" {
		if err := json.Unmarshal(payload.Payload, &itemPayload); err != nil {
			return TurnItem{}, fmt.Errorf("decode completed item %q payload: %w", payload.ID, err)
		}
	}
	item := TurnItem{
		ID: payload.ID, Kind: ItemKind(payload.Kind), Status: ItemStatus(payload.Status),
		CreatedAt: payload.CreatedAt, CompletedAt: payload.CompletedAt, Text: payload.Text,
		ToolName: payload.ToolName, CallID: payload.CallID, Payload: itemPayload, ToolResult: cloneToolResult(payload.ToolResult),
	}
	if err := item.Validate(); err != nil {
		return TurnItem{}, fmt.Errorf("validate completed item %q: %w", payload.ID, err)
	}
	return item, nil
}

func cloneToolResult(value *tool.ToolResult) *tool.ToolResult {
	if value == nil {
		return nil
	}
	cloned := value.Clone()
	return &cloned
}

// ProjectCompletedItems returns only durable completed items in rollout order.
// It intentionally ignores deltas and in-progress records so a resumed TUI
// cannot recreate a transient working animation.
func ProjectCompletedItems(lines []rollout.Line) ([]TurnItem, error) {
	items := make([]TurnItem, 0)
	for _, line := range lines {
		if line.Item.Kind != rollout.KindTurnItemCompleted {
			continue
		}
		item, err := DecodeCompletedItem(line)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// LegacyResponseItemsToCompleted is a narrow migration projection for old
// response_item lines. New writes must use TurnItemCompleted; this helper exists
// only so old rollouts remain renderable during Resume.
func LegacyResponseItemsToCompleted(lines []rollout.Line) ([]TurnItem, error) {
	items := make([]TurnItem, 0)
	for _, line := range lines {
		if line.Item.Kind != rollout.KindResponseItem {
			continue
		}
		payload, err := rollout.DecodeResponseItem(line.Item)
		if err != nil {
			return nil, fmt.Errorf("decode legacy response item at sequence %d: %w", line.Sequence, err)
		}
		kind := ItemAssistantMessage
		status := ItemStatusCompleted
		toolName := ""
		if payload.Type == rollout.ResponseToolCall || payload.Type == rollout.ResponseToolResult {
			kind = ItemToolCall
			toolName = payload.Name
			if payload.Type == rollout.ResponseToolResult {
				if payload.Status == "denied" {
					status = ItemDeclined
				} else if payload.Status == "failed" || payload.Status == "cancelled" {
					status = ItemFailed
				}
			}
		}
		if payload.Type != rollout.ResponseAssistantMessage && payload.Type != rollout.ResponseToolCall && payload.Type != rollout.ResponseToolResult {
			continue
		}
		created := line.Timestamp
		item := TurnItem{ID: fmt.Sprintf("legacy-%d", line.Sequence), Kind: kind, Status: status,
			CreatedAt: created, CompletedAt: created, Text: payload.Content, ToolName: toolName, CallID: payload.CallID}
		if err := item.Validate(); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
