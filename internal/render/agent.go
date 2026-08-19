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

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
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

func (renderer *AgentRenderer) Publish(ctx context.Context, runtimeEvent protocol.SessionEvent) error {
	if renderer == nil {
		return errors.New("Agent renderer is nil")
	}
	if ctx == nil {
		return errors.New("Agent renderer context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := runtimeEvent.Validate(); err != nil {
		return err
	}

	renderer.mutex.Lock()
	defer renderer.mutex.Unlock()
	switch typed := runtimeEvent.Message.(type) {
	case protocol.AssistantMessageDelta:
		return renderer.writeText(typed)
	case protocol.ItemStarted:
		if typed.Item.Kind == protocol.ItemAssistantMessage || typed.Item.Kind == protocol.ItemReasoning || typed.Item.ToolName == "update_plan" {
			return nil
		}
		return renderer.writeStatus("tool: %s (%s) started", typed.Item.ToolName, typed.Item.CallID)
	case protocol.ItemCompleted:
		if typed.Item.Kind == protocol.ItemAssistantMessage {
			return renderer.closeTurn(typed.Item.ID)
		}
		if typed.Item.Kind == protocol.ItemReasoning || typed.Item.ToolName == "update_plan" {
			return nil
		}
		status := "completed"
		if typed.Item.Status != protocol.ItemStatusCompleted {
			status = "failed"
		}
		return renderer.writeStatus("tool: %s (%s) %s: %s", typed.Item.ToolName, typed.Item.CallID, status, typed.Item.Text)
	case protocol.PlanUpdated:
		return renderer.writeStatus("plan: updated revision=%d items=%d", typed.Revision, len(typed.Items))
	case protocol.ThreadTokenUsageUpdated:
		return renderer.writeStatus("usage: input=%d cached=%d output=%d reasoning=%d total=%d", typed.Usage.InputTokens, typed.Usage.CachedInputTokens, typed.Usage.OutputTokens, typed.Usage.ReasoningTokens, typed.Usage.TotalTokens)
	case protocol.TurnStarted:
		return renderer.writeStatus("turn: started")
	case protocol.TurnCompleted:
		if typed.Summary != "" {
			return renderer.writeStatus("%s", typed.Summary)
		}
		return renderer.writeStatus("turn: completed: %s", typed.Error)
	case protocol.TurnAborted:
		if typed.Summary != "" {
			return renderer.writeStatus("%s", typed.Summary)
		}
		return renderer.writeStatus("turn: aborted: %s", typed.Reason)
	case protocol.Warning:
		return renderer.writeStatus("warning: %s", typed.Message)
	case protocol.StreamError:
		message := typed.Message
		if strings.TrimSpace(message) == "" {
			message = "request failed"
		}
		if typed.WillRetry {
			return renderer.writeStatus("%s", message)
		}
		return renderer.writeStatus("error: %s", message)
	case protocol.ReasoningDelta, protocol.CommandOutputDelta, protocol.ContextCompacted:
		return nil
	default:
		return nil
	}
}

func (renderer *AgentRenderer) writeText(delta protocol.AssistantMessageDelta) error {
	if delta.Delta == "" {
		return nil
	}
	written, err := io.WriteString(renderer.stdout, delta.Delta)
	if written > 0 {
		renderer.openTurns[delta.ItemID] = true
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

var _ protocol.EventSink = (*AgentRenderer)(nil)
