package app

import (
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type RolloutProjection struct {
	Items []protocol.TurnItem
	Usage llm.Usage
}

func ProjectRolloutItems(lines []rollout.Line) (RolloutProjection, error) {
	projection := RolloutProjection{Items: make([]protocol.TurnItem, 0)}
	for _, line := range lines {
		switch item := line.Item.(type) {
		case rollout.ResponseItem:
			if item.Type == rollout.ResponseUserMessage {
				projected := protocol.TurnItem{
					ID: protocol.ItemID(fmt.Sprintf("response-%d", line.Sequence)), Kind: protocol.ItemUserMessage,
					Status: protocol.ItemStatusCompleted, CreatedAt: line.Timestamp, CompletedAt: line.Timestamp,
					Text: strings.TrimSpace(item.Content), Payload: item,
				}
				if err := projected.Validate(); err != nil {
					return RolloutProjection{}, fmt.Errorf("project user response at sequence %d: %w", line.Sequence, err)
				}
				projection.Items = append(projection.Items, projected)
			}
		case rollout.EventMsgItem:
			if err := projection.applyEvent(line, item.Msg); err != nil {
				return RolloutProjection{}, err
			}
		case rollout.CompactedItem:
			projected := protocol.TurnItem{
				ID: protocol.ItemID(fmt.Sprintf("compaction-%d", line.Sequence)), Kind: protocol.ItemContextCompaction,
				Status: protocol.ItemStatusCompleted, CreatedAt: line.Timestamp, CompletedAt: line.Timestamp,
				Payload: item,
			}
			if err := projected.Validate(); err != nil {
				return RolloutProjection{}, fmt.Errorf("project compaction at sequence %d: %w", line.Sequence, err)
			}
			projection.Items = append(projection.Items, projected)
		}
	}
	return projection, nil
}

func (projection *RolloutProjection) applyEvent(line rollout.Line, message protocol.EventMsg) error {
	switch event := message.(type) {
	case protocol.ItemCompletedEvent:
		if err := event.Item.Validate(); err != nil {
			return fmt.Errorf("project completed item at sequence %d: %w", line.Sequence, err)
		}
		projection.Items = append(projection.Items, cloneTurnItem(event.Item))
	case protocol.TokenCountEvent:
		projection.Usage = addProjectedUsage(projection.Usage, event.Usage)
	case protocol.ContextCompactedEvent:
		itemID := event.ItemID
		if itemID == "" {
			itemID = protocol.ItemID(fmt.Sprintf("compaction-event-%d", line.Sequence))
		}
		projected := protocol.TurnItem{
			ID: itemID, Kind: protocol.ItemContextCompaction, Status: protocol.ItemStatusCompleted,
			CreatedAt: line.Timestamp, CompletedAt: line.Timestamp, Payload: event,
		}
		if err := projected.Validate(); err != nil {
			return fmt.Errorf("project context compaction at sequence %d: %w", line.Sequence, err)
		}
		projection.Items = append(projection.Items, projected)
	}
	return nil
}

func cloneTurnItem(item protocol.TurnItem) protocol.TurnItem {
	if item.ToolResult != nil {
		result := item.ToolResult.Clone()
		item.ToolResult = &result
	}
	return item
}

func addProjectedUsage(total, next llm.Usage) llm.Usage {
	total.InputTokens += next.InputTokens
	total.CachedInputTokens += next.CachedInputTokens
	total.OutputTokens += next.OutputTokens
	total.ReasoningTokens += next.ReasoningTokens
	total.TotalTokens += next.TotalTokens
	return total
}
