package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
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

func (renderer *InlineRenderer) Publish(ctx context.Context, event protocol.Event) error {
	if renderer == nil {
		return errors.New("inline renderer is nil")
	}
	if ctx == nil {
		return errors.New("inline renderer context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	renderer.mutex.Lock()
	defer renderer.mutex.Unlock()
	switch typed := event.Msg.(type) {
	case protocol.AgentMessageContentDeltaEvent:
		if typed.Delta == "" {
			return nil
		}
		if _, err := io.WriteString(renderer.output, typed.Delta); err != nil {
			return fmt.Errorf("write inline text: %w", err)
		}
		renderer.openText = true
		return nil
	case protocol.ItemCompletedEvent:
		if typed.Item.Kind == protocol.ItemAssistantMessage {
			return renderer.finishText()
		}
		if typed.Item.Kind == protocol.ItemPlan {
			if err := renderer.finishText(); err != nil {
				return err
			}
			for _, line := range NewProposedPlanCell(typed.Item.Text).RawLines() {
				if _, err := fmt.Fprintln(renderer.status, line); err != nil {
					return err
				}
			}
			return renderer.writeStatusBar()
		}
		return renderer.toolBlock(typed.Item, true)
	case protocol.PlanUpdateEvent:
		renderer.phase = "planning"
		return renderer.planBlock(typed)
	case protocol.ItemStartedEvent:
		renderer.phase = "working"
		renderer.toolCalls++
		return renderer.toolBlock(typed.Item, false)
	case protocol.TurnStartedEvent:
		renderer.phase = "starting"
		return renderer.statusLine("turn started")
	case protocol.TokenCountEvent:
		renderer.inputTokens = typed.Usage.InputTokens
		renderer.outputTokens = typed.Usage.OutputTokens
		total := typed.Usage.TotalTokens
		if total == 0 {
			total = typed.Usage.InputTokens + typed.Usage.OutputTokens
		}
		return renderer.statusLine("usage: input=%d output=%d total=%d", typed.Usage.InputTokens, typed.Usage.OutputTokens, total)
	case protocol.TurnCompleteEvent:
		renderer.phase = "idle"
		if typed.Summary != "" {
			return renderer.statusLine("%s", typed.Summary)
		}
		return renderer.statusLine("turn completed: %s", typed.Error)
	case protocol.TurnAbortedEvent:
		renderer.phase = "idle"
		if typed.Summary != "" {
			return renderer.statusLine("%s", typed.Summary)
		}
		return renderer.statusLine("turn aborted: %s", typed.Reason)
	case protocol.StreamErrorEvent:
		if typed.WillRetry {
			if typed.AdditionalDetails != nil && strings.TrimSpace(*typed.AdditionalDetails) != "" {
				return renderer.statusLine("%s — %s", typed.Message, *typed.AdditionalDetails)
			}
			return renderer.statusLine("%s", typed.Message)
		}
		renderer.phase = "error"
		return renderer.statusLine("error: %s", typed.Message)
	case protocol.WarningEvent:
		return renderer.statusLine("warning: %s", typed.Message)
	default:
		return nil
	}
}

func (renderer *InlineRenderer) planBlock(plan protocol.PlanUpdateEvent) error {
	if err := renderer.finishText(); err != nil {
		return err
	}
	for _, line := range rawStyledLines(NewPlanUpdateCell(plan).DisplayLines(rawToolContext())) {
		if _, err := fmt.Fprintln(renderer.status, sanitizeInlineEventText(line)); err != nil {
			return fmt.Errorf("write inline plan line: %w", err)
		}
	}
	return renderer.writeStatusBar()
}

func (renderer *InlineRenderer) toolBlock(item protocol.TurnItem, completed bool) error {
	if err := renderer.finishText(); err != nil {
		return err
	}
	if item.Kind == protocol.ItemCollabAgentToolCall {
		cell := newCollabAgentHistoryCell()
		started := item
		started.Status = protocol.ItemInProgress
		started.CompletedAt = time.Time{}
		cell.Apply(protocol.ItemStartedEvent{Item: started})
		if completed {
			cell.Apply(protocol.ItemCompletedEvent{Item: item})
		}
		for _, line := range cell.RawLines() {
			if _, err := fmt.Fprintln(renderer.status, sanitizeInlineEventText(line)); err != nil {
				return fmt.Errorf("write inline collaboration line: %w", err)
			}
		}
		return renderer.writeStatusBar()
	}
	cell := newToolHistoryCell()
	started := item
	started.Status = protocol.ItemInProgress
	started.CompletedAt = time.Time{}
	cell.Apply(protocol.ItemStartedEvent{Item: started})
	if completed {
		cell.Apply(protocol.ItemCompletedEvent{Item: item})
	}
	for _, line := range cell.RawLines() {
		if _, err := fmt.Fprintln(renderer.status, sanitizeInlineEventText(line)); err != nil {
			return fmt.Errorf("write inline tool line: %w", err)
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
		return "working"
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
