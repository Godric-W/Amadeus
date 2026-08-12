package render

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/Godric-W/Amadeus/internal/agent/event"
)

const maxAgentEventTextRunes = 240

var ansiControlSequence = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

type AgentRenderer struct {
	stdout    io.Writer
	stderr    io.Writer
	mutex     sync.Mutex
	openTurns map[string]bool
}

func NewAgentRenderer(stdout, stderr io.Writer) (*AgentRenderer, error) {
	if stdout == nil {
		return nil, errors.New("Agent renderer stdout is nil")
	}
	if stderr == nil {
		return nil, errors.New("Agent renderer stderr is nil")
	}
	return &AgentRenderer{stdout: stdout, stderr: stderr, openTurns: make(map[string]bool)}, nil
}

func (renderer *AgentRenderer) Publish(ctx context.Context, runtimeEvent event.Event) error {
	if renderer == nil {
		return errors.New("Agent renderer is nil")
	}
	if ctx == nil {
		return errors.New("Agent renderer context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if runtimeEvent == nil {
		return event.ErrNilEvent
	}

	renderer.mutex.Lock()
	defer renderer.mutex.Unlock()
	switch typed := runtimeEvent.(type) {
	case event.TextDelta:
		return renderer.writeText(typed)
	case event.LLMCallCompleted:
		return renderer.closeTurn(typed.LLMCallID)
	case event.ToolCallStarted:
		if typed.ToolName == "update_plan" {
			return nil
		}
		return renderer.writeStatus("tool: %s (%s) started", typed.ToolName, typed.CallID)
	case event.ToolCallCompleted:
		if typed.ToolName == "update_plan" {
			return nil
		}
		status := "completed"
		if !typed.Success {
			status = "failed"
		}
		partial := ""
		if typed.Partial {
			partial = " partial"
		}
		return renderer.writeStatus("tool: %s (%s) %s%s in %s: %s", typed.ToolName, typed.CallID, status, partial, typed.Duration, typed.Summary)
	case event.ApprovalRequested:
		return renderer.writeStatus("approval: requested for %s (%s, risk=%s): %s", typed.ToolName, typed.RequestID, typed.Risk, typed.Reason)
	case event.ApprovalResolved:
		return renderer.writeStatus("approval: %s for %s (%s, scope=%s, source=%s): %s", typed.Outcome, typed.ToolName, typed.RequestID, typed.Scope, typed.Source, typed.Reason)
	case event.UsageUpdated:
		return renderer.writeStatus("usage: input=%d cached=%d output=%d reasoning=%d total=%d", typed.Usage.InputTokens, typed.Usage.CachedInputTokens, typed.Usage.OutputTokens, typed.Usage.ReasoningTokens, typed.Usage.TotalTokens)
	case event.StatusChanged:
		return renderer.writeStatus("status: %s %s %s -> %s", typed.Entity, typed.EntityID, typed.From, typed.To)
	case event.TurnStatusChanged:
		return renderer.writeStatus("status: %s %s %s -> %s", typed.Entity, typed.EntityID, typed.From, typed.To)
	case event.DiagnosticPublished:
		return renderer.writeStatus("diagnostic[%s/%s]: %s", typed.Severity, typed.Code, typed.Message)
	case event.RunDiffUpdated:
		return nil
	case event.RunDiffInvalidated:
		return nil
	case event.PlanUpdated:
		return renderer.writeStatus("plan: updated revision=%d items=%d", typed.Revision, len(typed.Items))
	case event.TurnStarted:
		return renderer.writeStatus("run: started %s (task=%s)", typed.TurnID, typed.TaskID)
	case event.TurnCompleted:
		return renderer.writeStatus("run: %s (stop=%s): %s", typed.Status, typed.StopReason, typed.Reason)
	case event.ErrorOccurred:
		message := typed.Error.Message
		if strings.TrimSpace(message) == "" {
			message = "request failed"
		}
		return renderer.writeStatus("error: %s", message)
	case event.LLMCallStarted, event.ReasoningDelta:
		return nil
	default:
		return nil
	}
}

func (renderer *AgentRenderer) writeText(delta event.TextDelta) error {
	if delta.Delta == "" {
		return nil
	}
	written, err := io.WriteString(renderer.stdout, delta.Delta)
	if written > 0 {
		renderer.openTurns[delta.LLMCallID] = true
	}
	if err != nil {
		return fmt.Errorf("write Agent text delta: %w", err)
	}
	return nil
}

func (renderer *AgentRenderer) closeTurn(turnID string) error {
	if !renderer.openTurns[turnID] {
		return nil
	}
	if _, err := io.WriteString(renderer.stdout, "\n"); err != nil {
		return fmt.Errorf("write Agent completion newline: %w", err)
	}
	delete(renderer.openTurns, turnID)
	return nil
}

func (renderer *AgentRenderer) writeStatus(format string, arguments ...any) error {
	var outputErr error
	if len(renderer.openTurns) != 0 {
		if _, err := io.WriteString(renderer.stdout, "\n"); err != nil {
			outputErr = fmt.Errorf("write Agent status newline: %w", err)
		}
		clear(renderer.openTurns)
	}
	message := sanitizeAgentEventText(fmt.Sprintf(format, arguments...))
	if _, err := fmt.Fprintln(renderer.stderr, message); err != nil {
		outputErr = errors.Join(outputErr, fmt.Errorf("write Agent status: %w", err))
	}
	return outputErr
}

func sanitizeAgentEventText(value string) string {
	value = ansiControlSequence.ReplaceAllString(value, "")
	var result strings.Builder
	runes := 0
	space := false
	for len(value) > 0 && runes < maxAgentEventTextRunes {
		r, size := utf8.DecodeRuneInString(value)
		value = value[size:]
		if r == utf8.RuneError && size == 1 {
			r = '?'
		}
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			space = result.Len() > 0
			continue
		}
		if space {
			result.WriteByte(' ')
			space = false
		}
		result.WriteRune(r)
		runes++
	}
	if value != "" {
		result.WriteString("…")
	}
	return strings.TrimSpace(result.String())
}

var _ event.Sink = (*AgentRenderer)(nil)
