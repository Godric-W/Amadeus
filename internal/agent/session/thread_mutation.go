package session

import (
	"context"
	"errors"
	"time"

	"github.com/Godric-W/Amadeus/internal/contextmanager"
	"github.com/Godric-W/Amadeus/internal/llm"
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

func (session *Session) AppendSubagentNotification(ctx context.Context, agentID protocol.ThreadID, turnID protocol.TurnID, fragment contextmanager.ContextFragment) error {
	if session == nil {
		return errors.New("session is nil")
	}
	if fragment.Kind != "multi_agent.subagent_notification" || fragment.Role != llm.RoleUser || !fragment.Separate {
		return errors.New("subagent notification fragment is invalid")
	}
	message, err := fragment.ResponseItem()
	if err != nil {
		return err
	}
	item, err := rollout.NewEventMsgItem(protocol.SubagentNotificationEvent{AgentID: agentID, TurnID: turnID, Content: message.Content})
	if err != nil {
		return err
	}
	return session.appendItemsDurable(ctx, "", item)
}

func (session *Session) RecordAgentSpawnEdge(ctx context.Context, agentID protocol.ThreadID, state protocol.AgentSpawnEdgeState, at time.Time) error {
	if session == nil {
		return errors.New("session is nil")
	}
	item := rollout.AgentSpawnEdgeItem{
		AgentID: agentID, ParentThreadID: session.threadID, State: state, UpdatedAt: at.UTC(),
	}
	if err := item.Validate(); err != nil {
		return err
	}
	return session.appendItemsDurable(ctx, "", item)
}
