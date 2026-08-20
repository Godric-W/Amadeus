package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type toolEventObserver struct {
	appendItems   func(context.Context, protocol.TurnID, ...rollout.RolloutItem) error
	turnID        protocol.TurnID
	events        protocol.EventSink
	mu            sync.Mutex
	presentations map[string]toolCallPresentation
}

type toolCallPresentation struct {
	actionSummary string
	detail        string
	sideEffect    tool.SideEffect
}

func (presentation toolCallPresentation) payload(duration time.Duration, partial bool) map[string]any {
	return map[string]any{
		"action_summary": presentation.actionSummary,
		"detail":         presentation.detail,
		"side_effect":    string(presentation.sideEffect),
		"duration":       duration.String(),
		"partial":        partial,
	}
}

func NewToolEventObserver(appendItems func(context.Context, protocol.TurnID, ...rollout.RolloutItem) error, turnID protocol.TurnID, events protocol.EventSink) tool.LifecycleObserver {
	return &toolEventObserver{appendItems: appendItems, turnID: turnID, events: events, presentations: map[string]toolCallPresentation{}}
}

func (observer *toolEventObserver) ToolCallStarted(ctx context.Context, spec tool.ToolSpec, call tool.ToolCall) error {
	if !eventPolicyForTool(call.Name).emitActivity {
		return nil
	}
	presentation := tool.PresentCall(spec, call)
	snapshot := toolCallPresentation{actionSummary: presentation.ActionSummary, detail: presentation.Detail, sideEffect: spec.SideEffect}
	observer.mu.Lock()
	observer.presentations[call.ID] = snapshot
	observer.mu.Unlock()
	item := protocol.TurnItem{ID: protocol.ItemID(call.ID), Kind: toolItemKind(call.Name, spec.SideEffect), Status: protocol.ItemInProgress, CreatedAt: time.Now().UTC(), ToolName: call.Name, CallID: call.ID, Payload: snapshot.payload(0, false)}
	if err := observer.events.Publish(ctx, protocol.Event{Msg: protocol.ItemStartedEvent{Item: item}}); err != nil {
		observer.mu.Lock()
		delete(observer.presentations, call.ID)
		observer.mu.Unlock()
		return err
	}
	return nil
}

func (observer *toolEventObserver) ToolCallCompleted(ctx context.Context, execution tool.ToolExecution) error {
	if observer == nil || observer.appendItems == nil || observer.events == nil {
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
	policy := eventPolicyForTool(execution.Call.Name)
	if !policy.emitActivity {
		completionCtx := context.WithoutCancel(ctx)
		if err := observer.appendItems(completionCtx, observer.turnID, responseItem); err != nil {
			return fmt.Errorf("persist tool completion: %w", err)
		}
		return nil
	}
	status := protocol.ItemStatusCompleted
	switch execution.Outcome.Status {
	case tool.ToolCallDenied:
		status = protocol.ItemDeclined
	case tool.ToolCallFailed, tool.ToolCallInterrupted:
		status = protocol.ItemFailed
	}
	now := time.Now().UTC()
	observer.mu.Lock()
	presentation := observer.presentations[execution.Call.ID]
	delete(observer.presentations, execution.Call.ID)
	observer.mu.Unlock()
	itemPayload := presentation.payload(execution.Outcome.Duration, execution.Output.Partial)
	turnItem := protocol.TurnItem{ID: protocol.ItemID(execution.Call.ID), Kind: toolItemKind(execution.Call.Name, ""), Status: status, CreatedAt: now, CompletedAt: now, Text: toolExecutionSummary(execution), ToolName: execution.Call.Name, CallID: execution.Call.ID, ToolResult: &result, Payload: itemPayload}
	completedItem, err := rollout.NewEventMsgItem(protocol.ItemCompletedEvent{Item: turnItem})
	if err != nil {
		return err
	}
	completionCtx := context.WithoutCancel(ctx)
	if err := observer.appendItems(completionCtx, observer.turnID, responseItem, completedItem); err != nil {
		return fmt.Errorf("persist tool completion: %w", err)
	}
	return observer.events.Publish(completionCtx, protocol.Event{Msg: protocol.ItemCompletedEvent{Item: turnItem}})
}

func toolItemKind(name string, effect tool.SideEffect) protocol.ItemKind {
	if name == "execute_command" || name == "write_stdin" {
		return protocol.ItemCommandExecution
	}
	if effect == tool.SideEffectWrite || name == "edit" || name == "write" {
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
