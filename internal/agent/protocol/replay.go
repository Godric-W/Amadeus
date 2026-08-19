package protocol

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/llm"
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
		ID: string(item.ID), Kind: string(item.Kind), Status: string(item.Status),
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
		ID: ItemID(payload.ID), Kind: ItemKind(payload.Kind), Status: ItemStatus(payload.Status),
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

type ThreadProjection struct {
	Items []TurnItem
	Usage llm.Usage
}

// ProjectThreadItems is the only canonical rollout-to-TUI replay projector.
// It preserves rollout sequence and never reconstructs transient working state.
func ProjectThreadItems(lines []rollout.Line) (ThreadProjection, error) {
	completedTurns := make(map[rollout.TurnID]bool)
	for _, line := range lines {
		if line.Item.Kind == rollout.KindTurnItemCompleted {
			completedTurns[line.TurnID] = true
		}
	}
	projection := ThreadProjection{Items: make([]TurnItem, 0)}
	for _, line := range lines {
		switch line.Item.Kind {
		case rollout.KindTurnItemCompleted:
			item, err := DecodeCompletedItem(line)
			if err != nil {
				return ThreadProjection{}, err
			}
			projection.Items = append(projection.Items, item)
		case rollout.KindResponseItem:
			item, visible, err := projectResponseItem(line, completedTurns[line.TurnID])
			if err != nil {
				return ThreadProjection{}, err
			}
			if visible {
				projection.Items = append(projection.Items, item)
			}
		case rollout.KindPlanUpdate:
			item, err := projectPlanItem(line)
			if err != nil {
				return ThreadProjection{}, err
			}
			projection.Items = append(projection.Items, item)
		case rollout.KindCompaction:
			item, err := projectCompactionItem(line)
			if err != nil {
				return ThreadProjection{}, err
			}
			projection.Items = append(projection.Items, item)
		case rollout.KindTokenUsage:
			usage, err := rollout.DecodePayload[rollout.TokenUsage](line.Item)
			if err != nil {
				return ThreadProjection{}, err
			}
			projection.Usage = llm.Usage{
				InputTokens: usage.InputTokens, CachedInputTokens: usage.CachedInputTokens,
				OutputTokens: usage.OutputTokens, ReasoningTokens: usage.ReasoningTokens, TotalTokens: usage.TotalTokens,
			}
		}
	}
	return projection, nil
}

func projectResponseItem(line rollout.Line, hasCompletedItems bool) (TurnItem, bool, error) {
	payload, err := rollout.DecodeResponseItem(line.Item)
	if err != nil {
		return TurnItem{}, false, fmt.Errorf("decode response item at sequence %d: %w", line.Sequence, err)
	}
	kind := ItemAssistantMessage
	status := ItemStatusCompleted
	toolName := ""
	switch payload.Type {
	case rollout.ResponseUserMessage:
		kind = ItemUserMessage
	case rollout.ResponseAssistantMessage:
		if hasCompletedItems {
			return TurnItem{}, false, nil
		}
	case rollout.ResponseToolCall, rollout.ResponseToolResult:
		if hasCompletedItems {
			return TurnItem{}, false, nil
		}
		kind = ItemToolCall
		toolName = payload.Name
		if payload.Type == rollout.ResponseToolResult {
			switch payload.Status {
			case "denied":
				status = ItemDeclined
			case "failed", "cancelled":
				status = ItemFailed
			}
		}
	default:
		return TurnItem{}, false, nil
	}
	text := strings.TrimSpace(payload.Content)
	if text == "" {
		text = strings.TrimSpace(payload.Reasoning)
	}
	if text == "" && payload.Result != nil {
		text = strings.TrimSpace(payload.Result.Display.Summary)
		if text == "" {
			text = strings.TrimSpace(payload.Result.Text)
		}
	}
	item := TurnItem{
		ID: ItemID(fmt.Sprintf("rollout-%d", line.Sequence)), Kind: kind, Status: status,
		CreatedAt: line.Timestamp, CompletedAt: line.Timestamp, Text: text,
		ToolName: toolName, CallID: payload.CallID, ToolResult: cloneToolResult(payload.Result), Payload: payload,
	}
	if err := item.Validate(); err != nil {
		return TurnItem{}, false, err
	}
	return item, true, nil
}

func projectPlanItem(line rollout.Line) (TurnItem, error) {
	snapshot, err := rollout.DecodePayload[plan.Snapshot](line.Item)
	if err != nil {
		return TurnItem{}, err
	}
	updatedAt := snapshot.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = line.Timestamp
	}
	items := make([]PlanItem, 0, len(snapshot.Items))
	for _, value := range snapshot.Items {
		items = append(items, PlanItem{Step: value.Step, Status: string(value.Status)})
	}
	payload := PlanUpdateEvent{
		ItemID: ItemID(fmt.Sprintf("plan-%d", line.Sequence)), Explanation: snapshot.Explanation,
		Items: items, Revision: snapshot.Revision, UpdatedAt: updatedAt,
	}
	return TurnItem{
		ID: payload.ItemID, Kind: ItemPlan, Status: ItemStatusCompleted,
		CreatedAt: updatedAt, CompletedAt: updatedAt, Text: snapshot.Explanation, Payload: payload,
	}, nil
}

func projectCompactionItem(line rollout.Line) (TurnItem, error) {
	value, err := rollout.DecodePayload[rollout.Compaction](line.Item)
	if err != nil {
		return TurnItem{}, err
	}
	return TurnItem{
		ID: ItemID(fmt.Sprintf("compaction-%d", line.Sequence)), Kind: ItemContextCompaction,
		Status: ItemStatusCompleted, CreatedAt: line.Timestamp, CompletedAt: line.Timestamp,
		Payload: value,
	}, nil
}
