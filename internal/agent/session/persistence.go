package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
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
	var result threadstore.AppendResult
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

func (session *Session) materialize(ctx context.Context, input threadstore.CreateInput) (threadstore.AppendResult, error) {
	meta := sessionMetaItem(input)
	session.appendMu.Lock()
	defer session.appendMu.Unlock()
	firstSequence := session.state.Context.NextSequence()
	if err := session.state.Context.ValidateRecord(firstSequence, meta); err != nil {
		return threadstore.AppendResult{}, fmt.Errorf("validate session metadata record: %w", err)
	}
	result, err := session.services.LiveThread.Materialize(ctx, input)
	if err != nil {
		return threadstore.AppendResult{}, err
	}
	if result.Count == 0 {
		return result, nil
	}
	if err := session.recordAppendResult(result, firstSequence, []rollout.RolloutItem{meta}); err != nil {
		session.cancel(fmt.Errorf("record persisted session metadata: %w", err))
		return threadstore.AppendResult{}, err
	}
	return result, nil
}

func (session *Session) recordAppendResult(result threadstore.AppendResult, expectedFirst uint64, items []rollout.RolloutItem) error {
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
