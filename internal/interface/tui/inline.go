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

	"github.com/Godric-W/Amadeus/internal/agent/event"
)

const maxInlineEventTextRunes = 240

var inlineSensitiveValue = regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*bearer\s+\S+|\b(?:api[_-]?key|token|password)\s*[:=]\s*\S+|\bbearer\s+\S+`)

type InlineRenderer struct {
	output       io.Writer
	status       io.Writer
	mutex        sync.Mutex
	openText     bool
	phase        string
	currentTask  string
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

func (renderer *InlineRenderer) Publish(ctx context.Context, runtimeEvent event.Event) error {
	if renderer == nil {
		return errors.New("inline renderer is nil")
	}
	if ctx == nil {
		return errors.New("inline renderer context is nil")
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
		if typed.Delta == "" {
			return nil
		}
		if _, err := io.WriteString(renderer.output, typed.Delta); err != nil {
			return fmt.Errorf("write inline text: %w", err)
		}
		renderer.openText = true
		return nil
	case event.TurnCompleted:
		return renderer.finishText()
	case event.PlanUpdated:
		renderer.phase = "planning"
		if typed.Cycle > 1 {
			renderer.phase = "replanning"
		}
		return renderer.planBlock(typed)
	case event.EngineRunStarted:
		renderer.phase = "starting"
		renderer.currentTask = typed.TaskID
		return renderer.statusLine("run started: %s (task=%s)", typed.RunID, typed.TaskID)
	case event.EngineStatusChanged:
		if typed.Entity == "run" {
			renderer.phase = inlinePhase(typed.To)
		} else if typed.Entity == "task" {
			renderer.currentTask = typed.EntityID
			renderer.phase = "executing"
		}
		return renderer.statusLine("%s %s: %s -> %s", typed.Entity, typed.EntityID, typed.From, typed.To)
	case event.ToolCallStarted:
		renderer.phase = "executing"
		renderer.toolCalls++
		return renderer.statusLine("tool started: %s", typed.ToolName)
	case event.ToolCallCompleted:
		state := "completed"
		if !typed.Success {
			state = "failed"
		}
		partial := ""
		if typed.Partial {
			partial = " partial"
		}
		return renderer.statusLine("tool %s: %s%s in %s: %s", state, typed.ToolName, partial, typed.Duration, typed.Summary)
	case event.ApprovalRequested:
		renderer.phase = "awaiting_approval"
		return renderer.statusLine("approval required: %s (risk=%s): %s", typed.ToolName, typed.Risk, typed.Reason)
	case event.ApprovalResolved:
		renderer.phase = "executing"
		return renderer.statusLine("approval %s: %s (scope=%s, source=%s)", typed.Outcome, typed.ToolName, typed.Scope, typed.Source)
	case event.UsageUpdated:
		renderer.inputTokens = typed.Usage.InputTokens
		renderer.outputTokens = typed.Usage.OutputTokens
		total := typed.Usage.TotalTokens
		if total == 0 {
			total = typed.Usage.InputTokens + typed.Usage.OutputTokens
		}
		return renderer.statusLine("usage: input=%d output=%d total=%d", typed.Usage.InputTokens, typed.Usage.OutputTokens, total)
	case event.VerificationCompleted:
		if typed.Passed {
			return renderer.statusLine("verification passed: %s", typed.TaskID)
		}
		return renderer.statusLine("verification failed: %s: %s", typed.TaskID, strings.Join(typed.EvidenceGaps, "; "))
	case event.ReflectionCompleted:
		return renderer.statusLine("reflection: %s (task=%s, scope=%s)", typed.Verdict, typed.TaskID, typed.Scope)
	case event.EngineRunCompleted:
		renderer.phase = "idle"
		return renderer.statusLine("run %s: %s", typed.Status, typed.Reason)
	case event.ErrorOccurred:
		renderer.phase = "error"
		return renderer.statusLine("error: %s", typed.Error.Message)
	case event.StatusChanged:
		return renderer.statusLine("%s %s: %s -> %s", typed.Entity, typed.EntityID, typed.From, typed.To)
	case event.DiagnosticPublished:
		return renderer.statusLine("diagnostic[%s/%s]: %s", typed.Severity, typed.Code, typed.Message)
	default:
		return nil
	}
}

func (renderer *InlineRenderer) planBlock(plan event.PlanUpdated) error {
	if err := renderer.finishText(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(renderer.status, "plan %d:\n", plan.Cycle); err != nil {
		return fmt.Errorf("write inline plan heading: %w", err)
	}
	for _, task := range plan.Tasks {
		if _, err := fmt.Fprintf(renderer.status, "  %s [%s]: %s\n", sanitizeInlineEventText(task.ID), sanitizeInlineEventText(task.Status), sanitizeInlineEventText(task.Objective)); err != nil {
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
	if _, err := fmt.Fprintf(renderer.status, "status: phase=%s task=%s tools=%d usage=%d/%d\n", sanitizeInlineEventText(renderer.phase), sanitizeInlineEventText(renderer.currentTask), renderer.toolCalls, renderer.inputTokens, renderer.outputTokens); err != nil {
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

var _ event.Sink = (*InlineRenderer)(nil)
