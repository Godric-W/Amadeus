package thread

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/rollout"
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
	items := make([]rollout.Item, 0, len(pending)+1)
	for _, call := range pending {
		item, err := rollout.NewRawItem(rollout.KindResponseItem, mustEncode(map[string]any{
			"type": "tool_result", "call_id": call.ID, "name": call.Name,
			"status": "cancelled", "content": "Tool call cancelled because the previous process ended before completion.",
		}))
		if err != nil {
			return InitialHistory{}, err
		}
		items = append(items, item)
	}
	terminal, err := rollout.NewItem(rollout.KindTurnAborted, rollout.TurnAborted{Reason: "previous process ended before the turn completed"})
	if err != nil {
		return InitialHistory{}, err
	}
	items = append(items, terminal)
	result, err := live.AppendItems(ctx, turnID, items...)
	if err != nil {
		return InitialHistory{}, err
	}
	history.Lines = append(history.Lines, result.Lines...)
	return history, nil
}

type recoveredCall struct {
	ID   string
	Name string
}

func incompleteTurn(lines []rollout.Line) rollout.TurnID {
	open := make(map[rollout.TurnID]bool)
	for _, line := range lines {
		switch line.Item.Kind {
		case rollout.KindTurnStarted:
			open[line.TurnID] = true
		case rollout.KindTurnCompleted, rollout.KindTurnAborted:
			delete(open, line.TurnID)
		}
	}
	for index := len(lines) - 1; index >= 0; index-- {
		if lines[index].Item.Kind == rollout.KindTurnStarted && open[lines[index].TurnID] {
			return lines[index].TurnID
		}
	}
	return ""
}

func pendingToolCalls(lines []rollout.Line, turnID rollout.TurnID) []recoveredCall {
	calls := make([]recoveredCall, 0)
	seen := make(map[string]struct{})
	completed := make(map[string]struct{})
	for _, line := range lines {
		if line.TurnID != turnID || line.Item.Kind != rollout.KindResponseItem {
			continue
		}
		var payload struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			ID     string `json:"id"`
			Name   string `json:"name"`
		}
		if json.Unmarshal(line.Item.Payload, &payload) != nil {
			continue
		}
		callID := strings.TrimSpace(payload.CallID)
		if callID == "" {
			callID = strings.TrimSpace(payload.ID)
		}
		switch payload.Type {
		case "tool_call":
			if callID != "" {
				if _, exists := seen[callID]; !exists {
					seen[callID] = struct{}{}
					calls = append(calls, recoveredCall{ID: callID, Name: strings.TrimSpace(payload.Name)})
				}
			}
		case "tool_result":
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

func mustEncode(value any) json.RawMessage {
	content, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return content
}
