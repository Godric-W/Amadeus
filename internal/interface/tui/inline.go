package tui

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

const maxInlineEventTextRunes = 240

var inlineSensitiveValue = regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*bearer\s+\S+|\b(?:api[_-]?key|token|password)\s*[:=]\s*\S+|\bbearer\s+\S+`)

type InlineRenderer struct {
	output       io.Writer
	status       io.Writer
	mutex        sync.Mutex
	openText     bool
	phase        string
	toolCalls    int
	inputTokens  int64
	outputTokens int64
}

func NewInlineRenderer(output, status io.Writer) (*InlineRenderer, error) {
	if output == nil || status == nil {
		return nil, errors.New("inline renderer writers are nil")
	}
	return &InlineRenderer{output: output, status: status, phase: "idle"}, nil
}

func (renderer *InlineRenderer) Publish(ctx context.Context, runtimeEvent protocol.SessionEvent) error {
	if renderer == nil {
		return errors.New("inline renderer is nil")
	}
	if ctx == nil {
		return errors.New("inline renderer context is nil")
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
		if typed.Delta == "" {
			return nil
		}
		if _, err := io.WriteString(renderer.output, typed.Delta); err != nil {
			return fmt.Errorf("write inline text: %w", err)
		}
		renderer.openText = true
		return nil
	case protocol.ItemCompleted:
		if typed.Item.Kind == protocol.ItemAssistantMessage {
			return renderer.finishText()
		}
		if typed.Item.ToolName == "update_plan" {
			return nil
		}
		state := "completed"
		if typed.Item.Status != protocol.ItemStatusCompleted {
			state = string(typed.Item.Status)
		}
		return renderer.statusLine("tool %s: %s: %s", state, typed.Item.ToolName, typed.Item.Text)
	case protocol.PlanUpdated:
		renderer.phase = "planning"
		if typed.Revision > 1 {
			renderer.phase = "replanning"
		}
		return renderer.planBlock(typed)
	case protocol.ItemStarted:
		if typed.Item.ToolName == "update_plan" {
			return nil
		}
		renderer.phase = "executing"
		renderer.toolCalls++
		return renderer.statusLine("tool started: %s", typed.Item.ToolName)
	case protocol.TurnStarted:
		renderer.phase = "starting"
		return renderer.statusLine("turn started")
	case protocol.ThreadTokenUsageUpdated:
		renderer.inputTokens = typed.Usage.InputTokens
		renderer.outputTokens = typed.Usage.OutputTokens
		total := typed.Usage.TotalTokens
		if total == 0 {
			total = typed.Usage.InputTokens + typed.Usage.OutputTokens
		}
		return renderer.statusLine("usage: input=%d output=%d total=%d", typed.Usage.InputTokens, typed.Usage.OutputTokens, total)
	case protocol.TurnCompleted:
		renderer.phase = "idle"
		if typed.Summary != "" {
			return renderer.statusLine("%s", typed.Summary)
		}
		return renderer.statusLine("turn completed: %s", typed.Error)
	case protocol.TurnAborted:
		renderer.phase = "idle"
		if typed.Summary != "" {
			return renderer.statusLine("%s", typed.Summary)
		}
		return renderer.statusLine("turn aborted: %s", typed.Reason)
	case protocol.StreamError:
		renderer.phase = "error"
		return renderer.statusLine("error: %s", typed.Error)
	case protocol.Warning:
		return renderer.statusLine("warning: %s", typed.Message)
	default:
		return nil
	}
}

func (renderer *InlineRenderer) planBlock(plan protocol.PlanUpdated) error {
	if err := renderer.finishText(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(renderer.status, "plan %d:\n", plan.Revision); err != nil {
		return fmt.Errorf("write inline plan heading: %w", err)
	}
	for index, item := range plan.Items {
		if _, err := fmt.Fprintf(renderer.status, "  plan-%d [%s]: %s\n", index+1, sanitizeInlineEventText(item.Status), sanitizeInlineEventText(item.Step)); err != nil {
			return fmt.Errorf("write inline plan task: %w", err)
		}
	}
	return renderer.writeStatusBar()
}

func (renderer *InlineRenderer) finishText() error {
	if !renderer.openText {
		return nil
	}
	if _, err := io.WriteString(renderer.output, "\n"); err != nil {
		return fmt.Errorf("write inline text newline: %w", err)
	}
	renderer.openText = false
	return nil
}

func (renderer *InlineRenderer) statusLine(format string, args ...any) error {
	if err := renderer.finishText(); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(renderer.status, sanitizeInlineEventText(fmt.Sprintf(format, args...))); err != nil {
		return fmt.Errorf("write inline status: %w", err)
	}
	return renderer.writeStatusBar()
}

func (renderer *InlineRenderer) writeStatusBar() error {
	if _, err := fmt.Fprintf(renderer.status, "status: phase=%s tools=%d usage=%d/%d\n", sanitizeInlineEventText(renderer.phase), renderer.toolCalls, renderer.inputTokens, renderer.outputTokens); err != nil {
		return fmt.Errorf("write inline status bar: %w", err)
	}
	return nil
}

func inlinePhase(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "planning":
		return "planning"
	case "scheduling", "task_running", "running":
		return "executing"
	case "cancelled", "cancelling":
		return "cancelling"
	case "failed":
		return "error"
	case "completed":
		return "idle"
	default:
		return "idle"
	}
}

func sanitizeInlineEventText(value string) string {
	value = inlineSensitiveValue.ReplaceAllString(value, "[REDACTED]")
	var result strings.Builder
	runes := 0
	space := false
	for len(value) > 0 && runes < maxInlineEventTextRunes {
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

var _ protocol.EventSink = (*InlineRenderer)(nil)
