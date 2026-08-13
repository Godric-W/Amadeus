package react

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type toolEventObserver struct {
	events protocol.EventSink
}

func NewToolEventObserver(events protocol.EventSink) tool.LifecycleObserver {
	if events == nil {
		return nil
	}
	return &toolEventObserver{events: events}
}

func (observer *toolEventObserver) ToolCallStarted(ctx context.Context, spec tool.ToolSpec, call tool.ToolCall) error {
	presentation := tool.PresentCall(spec, call)
	kind := protocol.ItemToolCall
	if call.Name == "execute_command" || call.Name == "write_stdin" {
		kind = protocol.ItemCommandExecution
	} else if spec.SideEffect == tool.SideEffectWrite {
		kind = protocol.ItemFileChange
	}
	item := protocol.TurnItem{ID: call.ID, Kind: kind, Status: protocol.ItemInProgress, CreatedAt: time.Now().UTC(), ToolName: call.Name, CallID: call.ID, Payload: map[string]any{
		"action_summary": presentation.ActionSummary,
		"detail":         presentation.Detail,
		"side_effect":    string(spec.SideEffect),
	}}
	return observer.events.Publish(ctx, protocol.SessionEvent{Message: protocol.ItemStarted{Item: item}})
}

func (observer *toolEventObserver) ToolCallCompleted(ctx context.Context, execution tool.ToolExecution) error {
	status := protocol.ItemStatusCompleted
	switch execution.Outcome.Status {
	case tool.ToolCallDenied:
		status = protocol.ItemDeclined
	case tool.ToolCallFailed, tool.ToolCallInterrupted:
		status = protocol.ItemFailed
	}
	now := time.Now().UTC()
	item := protocol.TurnItem{ID: execution.Call.ID, Kind: toolItemKind(execution.Call.Name), Status: status, CreatedAt: now, CompletedAt: now, Text: toolExecutionSummary(execution), ToolName: execution.Call.Name, CallID: execution.Call.ID, Payload: map[string]any{
		"duration": execution.Outcome.Duration.String(),
		"partial":  execution.Output.Partial,
	}}
	return observer.events.Publish(context.WithoutCancel(ctx), protocol.SessionEvent{Message: protocol.ItemCompleted{Item: item}})
}

func toolItemKind(name string) protocol.ItemKind {
	if name == "execute_command" || name == "write_stdin" {
		return protocol.ItemCommandExecution
	}
	if name == "edit" || name == "write" || name == "apply_patch" {
		return protocol.ItemFileChange
	}
	return protocol.ItemToolCall
}

func toolExecutionSummary(execution tool.ToolExecution) string {
	if summary := strings.TrimSpace(execution.Output.Text); summary != "" {
		return summary
	}
	if execution.Outcome.Error != nil && strings.TrimSpace(execution.Outcome.Error.Message) != "" {
		return execution.Outcome.Error.Message
	}
	if len(execution.Output.Parts) != 0 {
		return fmt.Sprintf("tool returned %d content part(s)", len(execution.Output.Parts))
	}
	if execution.Output.Partial {
		return "tool completed with partial output"
	}
	return "tool completed"
}

func outcomeSummary(outcome ToolOutcome) string {
	if summary := strings.TrimSpace(outcome.Result.Text); summary != "" {
		return summary
	}
	if outcome.Error != nil && strings.TrimSpace(outcome.Error.Message) != "" {
		return outcome.Error.Message
	}
	if len(outcome.Result.Parts) != 0 {
		return fmt.Sprintf("tool returned %d content part(s)", len(outcome.Result.Parts))
	}
	if outcome.Partial {
		return "tool completed with partial output"
	}
	return "tool completed"
}

func projectToolExecution(execution tool.ToolExecution) ToolOutcome {
	status := ToolOutcomeFailed
	switch execution.Outcome.Status {
	case tool.ToolCallCompleted:
		status = ToolOutcomeSucceeded
	case tool.ToolCallDenied:
		status = ToolOutcomeDenied
	case tool.ToolCallInterrupted:
		status = ToolOutcomeInterrupted
	case tool.ToolCallFailed:
		status = ToolOutcomeFailed
	}
	var toolError *ToolError
	if execution.Outcome.Error != nil {
		toolError = &ToolError{Kind: execution.Outcome.Error.Kind, Message: execution.Outcome.Error.Message}
	}
	artifacts := make([]ArtifactRef, 0, len(execution.Output.Artifacts))
	for _, artifact := range execution.Output.Artifacts {
		artifacts = append(artifacts, ArtifactRef{Path: artifact.Path, URI: artifact.URI, Digest: artifact.Digest})
	}
	return ToolOutcome{
		CallID: execution.Call.ID, ToolName: execution.Call.Name, Status: status,
		Result: execution.Output.Clone(), Error: toolError, Blocking: execution.Outcome.Blocking,
		Partial: execution.Output.Partial, Duration: execution.Outcome.Duration,
		Artifacts: artifacts, Metadata: cloneToolMetadata(execution.Outcome.Metadata),
	}
}

func cloneToolMetadata(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
