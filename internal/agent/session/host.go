package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/thread"
)

func (session *Session) AppendItems(ctx context.Context, turnID protocol.TurnID, items ...rollout.RolloutItem) error {
	return session.appendItems(ctx, turnID, false, items...)
}

func (session *Session) appendItemsDurable(ctx context.Context, turnID protocol.TurnID, items ...rollout.RolloutItem) error {
	return session.appendItems(ctx, turnID, true, items...)
}

func (session *Session) appendItems(ctx context.Context, turnID protocol.TurnID, durable bool, items ...rollout.RolloutItem) error {
	if session == nil {
		return errors.New("session is nil")
	}
	if ctx == nil {
		return errors.New("append rollout context is nil")
	}
	scoped := make([]rollout.RolloutItem, len(items))
	for index, item := range items {
		scoped[index] = rollout.ScopeItem(item, session.threadID, turnID)
	}
	session.appendMu.Lock()
	defer session.appendMu.Unlock()
	firstSequence := session.state.Context.NextSequence()
	if err := session.state.Context.ValidateRecord(firstSequence, scoped...); err != nil {
		return fmt.Errorf("validate context record: %w", err)
	}
	var result thread.AppendResult
	var err error
	if durable {
		result, err = session.services.LiveThread.AppendItems(ctx, turnID, scoped...)
	} else {
		result, err = session.services.LiveThread.AppendItemsBuffered(ctx, turnID, scoped...)
	}
	if err != nil {
		return err
	}
	if err := session.recordAppendResult(result, firstSequence, scoped); err != nil {
		session.cancel(fmt.Errorf("record persisted rollout: %w", err))
		return err
	}
	if result.MetadataWarning != nil {
		session.publish(protocol.Event{Msg: protocol.WarningEvent{ThreadID: session.threadID, TurnID: turnID, Message: result.MetadataWarning.Error()}})
	}
	return nil
}

func (session *Session) materialize(ctx context.Context, input thread.CreateInput) (thread.AppendResult, error) {
	meta := sessionMetaItem(input)
	session.appendMu.Lock()
	defer session.appendMu.Unlock()
	firstSequence := session.state.Context.NextSequence()
	if err := session.state.Context.ValidateRecord(firstSequence, meta); err != nil {
		return thread.AppendResult{}, fmt.Errorf("validate session metadata record: %w", err)
	}
	result, err := session.services.LiveThread.Materialize(ctx, input)
	if err != nil {
		return thread.AppendResult{}, err
	}
	if result.Count == 0 {
		return result, nil
	}
	if err := session.recordAppendResult(result, firstSequence, []rollout.RolloutItem{meta}); err != nil {
		session.cancel(fmt.Errorf("record persisted session metadata: %w", err))
		return thread.AppendResult{}, err
	}
	return result, nil
}

func (session *Session) recordAppendResult(result thread.AppendResult, expectedFirst uint64, items []rollout.RolloutItem) error {
	if result.Count != len(items) {
		return fmt.Errorf("append receipt count is %d, expected %d", result.Count, len(items))
	}
	if len(items) == 0 {
		return nil
	}
	if result.FirstSequence != expectedFirst {
		return fmt.Errorf("append receipt starts at sequence %d, expected %d", result.FirstSequence, expectedFirst)
	}
	if err := session.state.Context.Record(result.FirstSequence, items...); err != nil {
		return fmt.Errorf("record context facts: %w", err)
	}
	return nil
}

func (session *Session) Snapshot(model llm.ModelInfo, prompt llm.Prompt) agentcontext.PromptSnapshot {
	if session == nil || session.state.Context == nil {
		return agentcontext.PromptSnapshot{}
	}
	return session.state.Context.Snapshot(model, prompt)
}

func (session *Session) ContextUpdate(key agentcontext.UpdateKey) string {
	if session == nil || session.state.Context == nil {
		return ""
	}
	return session.state.Context.Update(key)
}

func (session *Session) ContextProjection() agentcontext.RolloutMessageProjection {
	if session == nil || session.state.Context == nil {
		return agentcontext.RolloutMessageProjection{}
	}
	return session.state.Context.Projection()
}

func (session *Session) RolloutItemCount() int {
	if session == nil || session.state.Context == nil {
		return 0
	}
	return session.state.Context.RolloutItemCount()
}

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
