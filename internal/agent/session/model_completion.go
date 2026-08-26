package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func persistAssistantResponse(ctx context.Context, appendItems func(context.Context, protocol.TurnID, ...rollout.RolloutItem) error, turnID protocol.TurnID, message llm.ResponseItem, normalized []tool.ToolCall) error {
	items := make([]rollout.RolloutItem, 0, len(normalized)+1)
	if strings.TrimSpace(message.Content) != "" || strings.TrimSpace(message.Reasoning) != "" {
		item, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseAssistantMessage, Role: string(llm.RoleAssistant), Content: strings.TrimSpace(message.Content), Reasoning: strings.TrimSpace(message.Reasoning)})
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	for _, call := range normalized {
		item, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseToolCall, Role: string(llm.RoleAssistant), CallID: call.ID, Name: call.Name, Arguments: append([]byte(nil), call.Payload...)})
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return errors.New("model response has no persistable items")
	}
	return appendItems(ctx, turnID, items...)
}

func publishModelCompletions(ctx context.Context, appendItems func(context.Context, protocol.TurnID, ...rollout.RolloutItem) error, turnID protocol.TurnID, events protocol.EventSink, sampleID string, message llm.ResponseItem) error {
	if strings.TrimSpace(message.Content) != "" {
		if err := persistAndPublishModelCompletion(ctx, appendItems, turnID, events, sampleID+":assistant", protocol.ItemAssistantMessage, message.Content); err != nil {
			return err
		}
	}
	if strings.TrimSpace(message.Reasoning) != "" {
		if err := persistAndPublishModelCompletion(ctx, appendItems, turnID, events, sampleID+":reasoning", protocol.ItemReasoning, message.Reasoning); err != nil {
			return err
		}
	}
	return nil
}

func publishPlanModeCompletions(ctx context.Context, appendItems func(context.Context, protocol.TurnID, ...rollout.RolloutItem) error, turnID protocol.TurnID, events protocol.EventSink, sampleID string, message llm.ResponseItem, assistantText, planText string) error {
	if strings.TrimSpace(assistantText) != "" {
		if err := persistAndPublishModelCompletion(ctx, appendItems, turnID, events, sampleID+":assistant", protocol.ItemAssistantMessage, assistantText); err != nil {
			return err
		}
	}
	if strings.TrimSpace(message.Reasoning) != "" {
		if err := persistAndPublishModelCompletion(ctx, appendItems, turnID, events, sampleID+":reasoning", protocol.ItemReasoning, message.Reasoning); err != nil {
			return err
		}
	}
	if strings.TrimSpace(planText) == "" {
		return errors.New("proposed plan block is empty")
	}
	return persistAndPublishModelCompletion(ctx, appendItems, turnID, events, sampleID+":plan", protocol.ItemPlan, strings.TrimSpace(planText))
}

func persistAndPublishModelCompletion(ctx context.Context, appendItems func(context.Context, protocol.TurnID, ...rollout.RolloutItem) error, turnID protocol.TurnID, events protocol.EventSink, id string, kind protocol.ItemKind, text string) error {
	now := time.Now().UTC()
	turnItem := protocol.TurnItem{ID: protocol.ItemID(id), Kind: kind, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: text}
	item, err := rollout.NewEventMsgItem(protocol.ItemCompletedEvent{Item: turnItem})
	if err != nil {
		return err
	}
	completionCtx := context.WithoutCancel(ctx)
	if err := appendItems(completionCtx, turnID, item); err != nil {
		return fmt.Errorf("persist model completion: %w", err)
	}
	return events.Publish(completionCtx, protocol.Event{Msg: protocol.ItemCompletedEvent{Item: turnItem}})
}
