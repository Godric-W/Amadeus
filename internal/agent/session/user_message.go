package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func completedUserMessageItem(id protocol.ItemID, content, clientID string, at time.Time) protocol.TurnItem {
	return protocol.TurnItem{
		ID: id, Kind: protocol.ItemUserMessage, Status: protocol.ItemStatusCompleted,
		CreatedAt: at, CompletedAt: at, Text: content, ClientUserMessageID: strings.TrimSpace(clientID),
	}
}

func (session *Session) recordUserTurnInput(ctx context.Context, turnID protocol.TurnID, events protocol.EventSink, input UserTurnInput) error {
	if err := input.validate(); err != nil {
		return err
	}
	now := session.services.Clock().UTC()
	responseItem, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: string(llm.RoleUser), Content: input.Content})
	if err != nil {
		return err
	}
	turnItem := completedUserMessageItem(protocol.ItemID(session.services.NextID("item")), input.Content, input.ClientID, now)
	completedItem, err := rollout.NewEventMsgItem(protocol.ItemCompletedEvent{ThreadID: session.threadID, TurnID: turnID, Item: turnItem})
	if err != nil {
		return err
	}
	if err := session.appendItemsDurable(ctx, turnID, responseItem, completedItem); err != nil {
		return fmt.Errorf("persist user turn input: %w", err)
	}
	if events == nil {
		return nil
	}
	if err := events.Publish(context.WithoutCancel(ctx), protocol.Event{Msg: protocol.ItemCompletedEvent{Item: turnItem}}); err != nil {
		return fmt.Errorf("publish user turn input: %w", err)
	}
	return nil
}
