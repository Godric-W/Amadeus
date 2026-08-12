package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/react"
	"github.com/Godric-W/Amadeus/internal/agent/task"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type turnRolloutRecorder struct {
	host   task.Host
	turnID turn.ID
}

func (recorder *turnRolloutRecorder) RecordToolCalls(ctx context.Context, message llm.Message) error {
	if recorder == nil || recorder.host == nil || recorder.turnID == "" {
		return errors.New("turn rollout recorder is not configured")
	}
	if message.Role != llm.RoleAssistant || len(message.ToolCalls) == 0 {
		return errors.New("tool call rollout requires an assistant message with calls")
	}
	items := make([]rollout.Item, 0, len(message.ToolCalls)+1)
	content := strings.TrimSpace(message.Content)
	if content != "" {
		item, err := newResponseItem(map[string]any{"type": "assistant_message", "role": "assistant", "content": content})
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	for _, call := range message.ToolCalls {
		item, err := newResponseItem(map[string]any{
			"type": "tool_call", "role": "assistant", "call_id": call.ID,
			"name": call.Name, "arguments": json.RawMessage(call.Arguments),
		})
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	return recorder.host.AppendItems(ctx, recorder.turnID, items...)
}

func (recorder *turnRolloutRecorder) RecordToolOutcomes(ctx context.Context, outcomes []react.ToolOutcome) error {
	if recorder == nil || recorder.host == nil || recorder.turnID == "" {
		return errors.New("turn rollout recorder is not configured")
	}
	items := make([]rollout.Item, 0, len(outcomes))
	for _, outcome := range outcomes {
		payload := map[string]any{
			"type": "tool_result", "call_id": outcome.CallID, "name": outcome.ToolName,
			"status": outcome.Status, "content": outcome.Result.Text, "metadata": outcome.Metadata,
			"partial": outcome.Partial, "duration_nanos": int64(outcome.Duration),
		}
		if len(outcome.Result.Parts) > 0 {
			payload["parts"] = outcome.Result.Parts
		}
		if outcome.Error != nil {
			payload["error"] = outcome.Error
		}
		item, err := newResponseItem(payload)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil
	}
	return recorder.host.AppendItems(ctx, recorder.turnID, items...)
}

func recordPlanUpdate(ctx context.Context, host task.Host, turnID turn.ID, snapshot plan.Snapshot) error {
	item, err := rollout.NewRawItem(rollout.KindPlanUpdate, mustMarshalRaw(map[string]any{
		"explanation": snapshot.Explanation, "items": snapshot.Items,
		"updated_at": snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano), "revision": snapshot.Revision,
	}))
	if err != nil {
		return err
	}
	return host.AppendItems(ctx, turnID, item)
}

func newResponseItem(payload any) (rollout.Item, error) {
	return rollout.NewRawItem(rollout.KindResponseItem, mustMarshalRaw(payload))
}

func mustMarshalRaw(value any) json.RawMessage {
	content, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return content
}
