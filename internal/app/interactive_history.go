package app

import (
	"fmt"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type RolloutProjection struct {
	Items                  []protocol.TurnItem
	TokenInfo              *protocol.TokenUsageInfo
	ActiveContextTokens    int64
	ActiveContextEstimated bool
}

func ProjectRolloutItems(lines []rollout.Line) (RolloutProjection, error) {
	projection := RolloutProjection{Items: make([]protocol.TurnItem, 0)}
	for index, line := range lines {
		switch item := line.Item.(type) {
		case rollout.ResponseItem:
			if item.Type == rollout.ResponseUserMessage {
				if responseHasCanonicalUserItem(lines, index, item) {
					continue
				}
				projected := protocol.TurnItem{
					ID: protocol.ItemID(fmt.Sprintf("response-%d", line.Sequence)), Kind: protocol.ItemUserMessage,
					Status: protocol.ItemStatusCompleted, CreatedAt: line.Timestamp, CompletedAt: line.Timestamp,
					Text: item.Content, Payload: item,
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
				Payload: protocol.ContextCompactionItem{Trigger: item.Trigger, Reason: item.Reason, Phase: item.Phase},
			}
			if err := projected.Validate(); err != nil {
				return RolloutProjection{}, fmt.Errorf("project compaction at sequence %d: %w", line.Sequence, err)
			}
			projection.Items = append(projection.Items, projected)
		}
	}
	return projection, nil
}

func responseHasCanonicalUserItem(lines []rollout.Line, index int, response rollout.ResponseItem) bool {
	if index+1 >= len(lines) {
		return false
	}
	eventItem, ok := lines[index+1].Item.(rollout.EventMsgItem)
	if !ok {
		return false
	}
	completed, ok := eventItem.Msg.(protocol.ItemCompletedEvent)
	if !ok || completed.Item.Kind != protocol.ItemUserMessage {
		return false
	}
	return completed.TurnID == response.TurnID && completed.Item.Text == response.Content
}

func (projection *RolloutProjection) applyEvent(line rollout.Line, message protocol.EventMsg) error {
	switch event := message.(type) {
	case protocol.ItemCompletedEvent:
		if err := event.Item.Validate(); err != nil {
			return fmt.Errorf("project completed item at sequence %d: %w", line.Sequence, err)
		}
		projection.Items = append(projection.Items, cloneTurnItem(event.Item))
	case protocol.TokenCountEvent:
		projection.TokenInfo = cloneTokenInfo(event.Info)
		projection.ActiveContextTokens = event.ActiveContextTokens
		projection.ActiveContextEstimated = event.ActiveContextEstimated
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

func cloneTokenInfo(info *protocol.TokenUsageInfo) *protocol.TokenUsageInfo {
	if info == nil {
		return nil
	}
	cloned := info.Clone()
	return &cloned
}
