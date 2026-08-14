package task

import (
	"context"
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/react"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type turnRolloutRecorder struct {
	host   Host
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
		item, err := rollout.NewResponseItem(rollout.ResponseItem{
			Type: rollout.ResponseAssistantMessage, Role: string(llm.RoleAssistant), Content: content, Reasoning: strings.TrimSpace(message.Reasoning),
		})
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	for _, call := range message.ToolCalls {
		item, err := rollout.NewResponseItem(rollout.ResponseItem{
			Type: rollout.ResponseToolCall, Role: string(llm.RoleAssistant), CallID: call.ID,
			Name: call.Name, Arguments: append([]byte(nil), call.Arguments...), Reasoning: strings.TrimSpace(message.Reasoning),
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
	items := make([]rollout.Item, 0, len(outcomes)*2)
	for _, outcome := range outcomes {
		result := outcome.Result.Clone()
		payload := rollout.ResponseItem{
			Type: rollout.ResponseToolResult, Role: string(llm.RoleTool), CallID: outcome.CallID, Name: outcome.ToolName,
			Status: string(outcome.Status), Content: result.Text, Result: &result, Metadata: outcome.Metadata,
			Partial: outcome.Partial, Duration: int64(outcome.Duration), Parts: append([]tool.ContentPart(nil), result.Parts...),
		}
		if outcome.Error != nil {
			payload.Error = &rollout.ResponseError{Kind: outcome.Error.Kind, Message: outcome.Error.Message}
		}
		item, err := rollout.NewResponseItem(payload)
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
