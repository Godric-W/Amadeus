package react

import (
	"context"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type toolEventObserver struct {
	events event.Sink
}

func NewToolEventObserver(events event.Sink) tool.LifecycleObserver {
	if events == nil {
		return nil
	}
	return &toolEventObserver{events: events}
}

func (observer *toolEventObserver) ToolCallStarted(ctx context.Context, spec tool.Spec, call tool.ToolCall) error {
	presentation := tool.PresentCall(spec, call)
	return observer.events.Publish(ctx, event.ToolCallStarted{
		CallID: call.ID, ToolName: call.Name, SideEffect: string(spec.SideEffect),
		ActionSummary: presentation.ActionSummary, Detail: presentation.Detail,
	})
}

func (observer *toolEventObserver) ToolCallCompleted(ctx context.Context, execution tool.ToolExecution) error {
	return observer.events.Publish(ctx, event.ToolCallCompleted{
		CallID: execution.Call.ID, ToolName: execution.Call.Name,
		Success: execution.Outcome.Status == tool.ToolCallCompleted, Partial: execution.Output.Partial,
		Summary: toolExecutionSummary(execution), Duration: execution.Outcome.Duration,
	})
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
	artifacts := make([]ArtifactRef, 0, len(execution.Outcome.Artifacts))
	for _, artifact := range execution.Outcome.Artifacts {
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
