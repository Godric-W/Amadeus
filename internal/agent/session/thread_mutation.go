package session

import (
	"context"
	"errors"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func (session *Session) Rename(ctx context.Context, title string, at time.Time) error {
	if session == nil {
		return errors.New("session is nil")
	}
	if at.IsZero() {
		return errors.New("session rename time is zero")
	}
	item := rollout.EventMsgItem{Msg: protocol.ThreadNameUpdatedEvent{Name: title}}
	return session.appendItemsDurable(ctx, "", item)
}

func (session *Session) AppendSubagentNotification(ctx context.Context, agentID protocol.ThreadID, content string) error {
	if session == nil {
		return errors.New("session is nil")
	}
	item, err := rollout.NewEventMsgItem(protocol.SubagentNotificationEvent{AgentID: agentID, Content: content})
	if err != nil {
		return err
	}
	return session.appendItemsDurable(ctx, "", item)
}
