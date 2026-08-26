package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type ContextWindowTokenStatus struct {
	ActiveContextTokens       int64
	EstimatedInputTokens      int64
	AutoCompactTokenLimit     int64
	FullContextWindowLimit    int64
	BaseWindowTokensRemaining int64
	TokenLimitReached         bool
}

func (session *Session) contextWindowTokenStatus(step StepContext) ContextWindowTokenStatus {
	model := step.Model.Normalized()
	estimated := step.Prompt.EstimatedInputTokens
	active, _ := session.state.Context.ActiveContextTokens(model)
	if active <= 0 {
		active = estimated
	}
	budgetUsed := max(active, estimated)
	remaining := minPositiveRemaining(model.AutoCompactTokenLimit, model.ContextWindow, budgetUsed)
	return ContextWindowTokenStatus{
		ActiveContextTokens: active, EstimatedInputTokens: estimated,
		AutoCompactTokenLimit: model.AutoCompactTokenLimit, FullContextWindowLimit: model.ContextWindow,
		BaseWindowTokensRemaining: remaining,
		TokenLimitReached: (model.AutoCompactTokenLimit > 0 && budgetUsed >= model.AutoCompactTokenLimit) ||
			(model.ContextWindow > 0 && budgetUsed >= model.ContextWindow),
	}
}

func minPositiveRemaining(compactLimit, contextWindow, used int64) int64 {
	remaining := int64(0)
	set := false
	for _, limit := range []int64{compactLimit, contextWindow} {
		if limit <= 0 {
			continue
		}
		candidate := max(int64(0), limit-used)
		if !set || candidate < remaining {
			remaining = candidate
			set = true
		}
	}
	return remaining
}

func (session *Session) recordTokenUsage(ctx context.Context, turnID protocol.TurnID, usage llm.TokenUsage, activeTokens, contextWindow int64, observedThrough uint64, events protocol.EventSink) error {
	if session == nil || session.state.Context == nil {
		return errors.New("token usage context is unavailable")
	}
	if usage.TotalTokens <= 0 {
		return nil
	}
	snapshot := session.state.Context.TokenSnapshot()
	info := protocol.TokenUsageInfo{}
	if snapshot.Info != nil {
		info = snapshot.Info.Clone()
	}
	info.Append(usage, contextWindow)
	if activeTokens <= 0 {
		activeTokens = usage.TotalTokens
	}
	event := protocol.TokenCountEvent{Info: &info, ActiveContextTokens: activeTokens, ObservedThroughSequence: observedThrough}
	item, err := rollout.NewEventMsgItem(event)
	if err != nil {
		return err
	}
	if err := session.AppendItems(ctx, turnID, item); err != nil {
		return err
	}
	if events != nil {
		return events.Publish(ctx, protocol.Event{Msg: event})
	}
	return nil
}

func (session *Session) refreshContextWindowStatus(ctx context.Context, turnID protocol.TurnID, step StepContext, events protocol.EventSink) (ContextWindowTokenStatus, error) {
	status := session.contextWindowTokenStatus(step)
	snapshot := session.state.Context.TokenSnapshot()
	_, estimated := session.state.Context.ActiveContextTokens(step.Model)
	if snapshot.ActiveContextTokens == status.ActiveContextTokens && snapshot.ActiveContextEstimated == estimated {
		return status, nil
	}
	event := protocol.TokenCountEvent{
		Info: cloneProtocolTokenUsageInfo(snapshot.Info), ActiveContextTokens: status.ActiveContextTokens,
		ActiveContextEstimated: estimated, ObservedThroughSequence: step.Prompt.HistoryVersion,
	}
	item, err := rollout.NewEventMsgItem(event)
	if err != nil {
		return ContextWindowTokenStatus{}, err
	}
	if err := session.AppendItems(ctx, turnID, item); err != nil {
		return ContextWindowTokenStatus{}, err
	}
	if events != nil {
		if err := events.Publish(ctx, protocol.Event{Msg: event}); err != nil {
			return ContextWindowTokenStatus{}, err
		}
	}
	return status, nil
}

func cloneProtocolTokenUsageInfo(info *protocol.TokenUsageInfo) *protocol.TokenUsageInfo {
	if info == nil {
		return nil
	}
	cloned := info.Clone()
	return &cloned
}
