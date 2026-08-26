package threadstore

import (
	"context"
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func RecoverInterruptedTurn(ctx context.Context, live *LiveThread, history InitialHistory) (InitialHistory, error) {
	if live == nil {
		return InitialHistory{}, errors.New("recovery live thread is nil")
	}
	turnID := incompleteTurn(history.Lines)
	if turnID == "" {
		return history, nil
	}
	pending := pendingToolCalls(history.Lines, turnID)
	items := make([]rollout.RolloutItem, 0, len(pending)+1)
	for _, call := range pending {
		message := "Tool call cancelled because the previous process ended before completion."
		item, err := rollout.NewResponseItem(rollout.ResponseItem{
			Type: rollout.ResponseToolResult, Role: "tool", CallID: call.ID,
			Name: call.Name, Status: "cancelled", Content: message,
			Result: &tool.ToolResult{CallID: call.ID, ToolName: call.Name, Text: message},
		})
		if err != nil {
			return InitialHistory{}, err
		}
		items = append(items, item)
	}
	terminal, err := rollout.NewEventMsgItem(protocol.TurnAbortedEvent{Reason: "previous process ended before the turn completed"})
	if err != nil {
		return InitialHistory{}, err
	}
	items = append(items, terminal)
	if _, err := live.AppendItems(ctx, turnID, items...); err != nil {
		return InitialHistory{}, err
	}
	return live.History(ctx)
}

type recoveredCall struct {
	ID   string
	Name string
}

func incompleteTurn(lines []rollout.Line) protocol.TurnID {
	open := make(map[protocol.TurnID]bool)
	for _, line := range lines {
		event, ok := line.Item.(rollout.EventMsgItem)
		if !ok {
			continue
		}
		switch message := event.Msg.(type) {
		case protocol.TurnStartedEvent:
			open[message.TurnID] = true
		case protocol.TurnCompleteEvent:
			delete(open, message.TurnID)
		case protocol.TurnAbortedEvent:
			delete(open, message.TurnID)
		}
	}
	for index := len(lines) - 1; index >= 0; index-- {
		event, ok := lines[index].Item.(rollout.EventMsgItem)
		if !ok {
			continue
		}
		if started, ok := event.Msg.(protocol.TurnStartedEvent); ok && open[started.TurnID] {
			return started.TurnID
		}
	}
	return ""
}

func pendingToolCalls(lines []rollout.Line, turnID protocol.TurnID) []recoveredCall {
	calls := make([]recoveredCall, 0)
	seen := make(map[string]struct{})
	completed := make(map[string]struct{})
	for _, line := range lines {
		item, ok := line.Item.(rollout.ResponseItem)
		if !ok || item.TurnID != turnID {
			continue
		}
		callID := strings.TrimSpace(item.CallID)
		switch item.Type {
		case rollout.ResponseToolCall:
			if callID != "" {
				if _, exists := seen[callID]; !exists {
					seen[callID] = struct{}{}
					calls = append(calls, recoveredCall{ID: callID, Name: strings.TrimSpace(item.Name)})
				}
			}
		case rollout.ResponseToolResult:
			if callID != "" {
				completed[callID] = struct{}{}
			}
		}
	}
	pending := calls[:0]
	for _, call := range calls {
		if _, ok := completed[call.ID]; !ok {
			pending = append(pending, call)
		}
	}
	return pending
}
