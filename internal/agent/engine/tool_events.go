package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type RolloutHost interface {
	AppendItems(context.Context, turn.ID, ...rollout.Item) error
}

type toolEventObserver struct {
	host   RolloutHost
	turnID turn.ID
	events protocol.EventSink
}

func newToolEventObserver(host RolloutHost, turnID turn.ID, events protocol.EventSink) tool.LifecycleObserver {
	return &toolEventObserver{host: host, turnID: turnID, events: events}
}

func (observer *toolEventObserver) ToolCallStarted(ctx context.Context, spec tool.ToolSpec, call tool.ToolCall) error {
	presentation := tool.PresentCall(spec, call)
	item := protocol.TurnItem{ID: call.ID, Kind: toolItemKind(call.Name, spec.SideEffect), Status: protocol.ItemInProgress, CreatedAt: time.Now().UTC(), ToolName: call.Name, CallID: call.ID, Payload: map[string]any{
		"action_summary": presentation.ActionSummary, "detail": presentation.Detail, "side_effect": string(spec.SideEffect),
	}}
	return observer.events.Publish(ctx, protocol.SessionEvent{Message: protocol.ItemStarted{Item: item}})
}

func (observer *toolEventObserver) ToolCallCompleted(ctx context.Context, execution tool.ToolExecution) error {
	if observer == nil || observer.host == nil || observer.events == nil {
		return errors.New("tool event observer is incomplete")
	}
	result := execution.Output.Clone()
	payload := rollout.ResponseItem{
		Type: rollout.ResponseToolResult, Role: string(llm.RoleTool), CallID: execution.Call.ID, Name: execution.Call.Name,
		Status: string(execution.Outcome.Status), Content: result.Text, Result: &result,
		Metadata: execution.Outcome.Metadata, Partial: result.Partial, Duration: int64(execution.Outcome.Duration),
		Parts: append([]tool.ContentPart(nil), result.Parts...),
	}
	if execution.Outcome.Error != nil {
		payload.Error = &rollout.ResponseError{Kind: execution.Outcome.Error.Kind, Message: execution.Outcome.Error.Message}
	}
	responseItem, err := rollout.NewResponseItem(payload)
	if err != nil {
		return err
	}
	status := protocol.ItemStatusCompleted
	switch execution.Outcome.Status {
	case tool.ToolCallDenied:
		status = protocol.ItemDeclined
	case tool.ToolCallFailed, tool.ToolCallInterrupted:
		status = protocol.ItemFailed
	}
	now := time.Now().UTC()
	turnItem := protocol.TurnItem{ID: execution.Call.ID, Kind: toolItemKind(execution.Call.Name, ""), Status: status, CreatedAt: now, CompletedAt: now, Text: toolExecutionSummary(execution), ToolName: execution.Call.Name, CallID: execution.Call.ID, ToolResult: &result, Payload: map[string]any{
		"duration": execution.Outcome.Duration.String(), "partial": execution.Output.Partial,
	}}
	completedItem, err := protocol.NewCompletedItem(turnItem)
	if err != nil {
		return err
	}
	completionCtx := context.WithoutCancel(ctx)
	if err := observer.host.AppendItems(completionCtx, observer.turnID, responseItem, completedItem); err != nil {
		return fmt.Errorf("persist tool completion: %w", err)
	}
	return observer.events.Publish(completionCtx, protocol.SessionEvent{Message: protocol.ItemCompleted{Item: turnItem}})
}

func toolItemKind(name string, effect tool.SideEffect) protocol.ItemKind {
	if name == "execute_command" || name == "write_stdin" {
		return protocol.ItemCommandExecution
	}
	if effect == tool.SideEffectWrite || name == "edit" || name == "write" || name == "apply_patch" {
		return protocol.ItemFileChange
	}
	return protocol.ItemToolCall
}

func toolExecutionSummary(execution tool.ToolExecution) string {
	if text := strings.TrimSpace(execution.Output.Text); text != "" {
		return text
	}
	if execution.Outcome.Error != nil && strings.TrimSpace(execution.Outcome.Error.Message) != "" {
		return execution.Outcome.Error.Message
	}
	if len(execution.Output.Parts) > 0 {
		return fmt.Sprintf("tool returned %d content part(s)", len(execution.Output.Parts))
	}
	if execution.Output.Partial {
		return "tool completed with partial output"
	}
	return "tool completed"
}
