package tui

import (
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

type HistoryRenderMode uint8

const (
	HistoryRenderRich HistoryRenderMode = iota
	HistoryRenderRaw
)

type semanticStyle uint8

const (
	stylePlain semanticStyle = iota
	styleDim
	styleBold
	styleAccent
	styleCommand
	styleSuccess
	styleFailure
	styleWarning
	styleUser
	styleSeparator
	styleRendered
)

type styledSpan struct {
	Text  string
	Style semanticStyle
}

type styledLine []styledSpan

type HistoryRenderContext struct {
	Width       int
	Palette     terminalPalette
	Markdown    *glamour.TermRenderer
	Now         time.Time
	MotionStart time.Time
	Motion      motionMode
}

type HistoryCell interface {
	DisplayLines(HistoryRenderContext) []styledLine
	RawLines() []string
	IsStreamContinuation() bool
}

type ActiveHistoryCell interface {
	HistoryCell
	Apply(protocol.EventMsg) bool
	Complete() HistoryCell
	IsComplete() bool
}

type TranscriptState struct {
	ActiveCell                 ActiveHistoryCell
	ActiveCellRevision         uint64
	NeedsFinalMessageSeparator bool
	HadWorkActivity            bool
	LastAgentMarkdown          string
}

func (state *TranscriptState) bumpActiveCellRevision() {
	state.ActiveCellRevision++
}

func (state *TranscriptState) reset() { *state = TranscriptState{} }

type UserMessageCell struct{ Content string }

func NewUserMessageCell(content string) HistoryCell {
	return UserMessageCell{Content: strings.TrimSpace(content)}
}

func (cell UserMessageCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	content := sanitizeFullscreenContent(cell.Content)
	if content == "" {
		return nil
	}
	wrapped := xansi.Hardwrap("› "+content, maxInt(12, ctx.Width), true)
	return styledLinesFromText(wrapped, styleUser)
}

func (cell UserMessageCell) RawLines() []string {
	if strings.TrimSpace(cell.Content) == "" {
		return nil
	}
	return strings.Split(cell.Content, "\n")
}

func (UserMessageCell) IsStreamContinuation() bool { return false }

type AgentMessageCell struct{ Markdown string }

func NewAgentMessageCell(markdown string) HistoryCell {
	return AgentMessageCell{Markdown: strings.TrimSpace(markdown)}
}

func (cell AgentMessageCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	content := sanitizeFullscreenContent(cell.Markdown)
	if content == "" {
		return nil
	}
	rendered := prefixRenderedBlock(content, "• ")
	if ctx.Markdown != nil {
		if markdown, err := ctx.Markdown.Render(content); err == nil {
			rendered = prefixRenderedBlock(markdown, "• ")
		}
	}
	return styledLinesFromText(rendered, styleRendered)
}

func (cell AgentMessageCell) RawLines() []string {
	if strings.TrimSpace(cell.Markdown) == "" {
		return nil
	}
	return strings.Split(cell.Markdown, "\n")
}

func (AgentMessageCell) IsStreamContinuation() bool { return false }

type PlainHistoryCell struct {
	Content string
	Style   semanticStyle
}

func NewPlainHistoryCell(content string) HistoryCell {
	return PlainHistoryCell{Content: strings.TrimSpace(content), Style: stylePlain}
}

func (cell PlainHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	return styledLinesFromText(sanitizeFullscreenContent(cell.Content), cell.Style)
}

func (cell PlainHistoryCell) RawLines() []string    { return rawLines(cell.Content) }
func (PlainHistoryCell) IsStreamContinuation() bool { return false }

type NoticeHistoryCell struct{ Content string }

func NewNoticeHistoryCell(content string) HistoryCell {
	return NoticeHistoryCell{Content: strings.TrimSpace(content)}
}

func (cell NoticeHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	return styledLinesFromText(sanitizeFullscreenContent(cell.Content), styleDim)
}

func (cell NoticeHistoryCell) RawLines() []string    { return rawLines(cell.Content) }
func (NoticeHistoryCell) IsStreamContinuation() bool { return false }

type DiagnosticHistoryCell struct{ Content string }

func NewDiagnosticHistoryCell(content string) HistoryCell {
	return DiagnosticHistoryCell{Content: strings.TrimSpace(content)}
}

func (cell DiagnosticHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	return styledLinesFromText(sanitizeFullscreenContent(cell.Content), styleDim)
}

func (cell DiagnosticHistoryCell) RawLines() []string    { return rawLines(cell.Content) }
func (DiagnosticHistoryCell) IsStreamContinuation() bool { return false }

type WarningHistoryCell struct{ Content string }

func NewWarningHistoryCell(content string) HistoryCell {
	return WarningHistoryCell{Content: strings.TrimSpace(content)}
}

func (cell WarningHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	if cell.Content == "" {
		return nil
	}
	return []styledLine{{{Text: "⚠ " + sanitizeFullscreenContent(cell.Content), Style: styleWarning}}}
}

func (cell WarningHistoryCell) RawLines() []string {
	if cell.Content == "" {
		return nil
	}
	return []string{"⚠ " + cell.Content}
}

func (WarningHistoryCell) IsStreamContinuation() bool { return false }

type ErrorHistoryCell struct{ Message string }

func NewErrorHistoryCell(message string) HistoryCell {
	return ErrorHistoryCell{Message: strings.TrimSpace(message)}
}

func (cell ErrorHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	if strings.TrimSpace(cell.Message) == "" {
		return nil
	}
	return []styledLine{{{Text: "Error: " + sanitizeFullscreenContent(cell.Message), Style: styleFailure}}}
}

func (cell ErrorHistoryCell) RawLines() []string {
	if strings.TrimSpace(cell.Message) == "" {
		return nil
	}
	return []string{"Error: " + cell.Message}
}

func (ErrorHistoryCell) IsStreamContinuation() bool { return false }

type FinalMessageSeparator struct{ Elapsed time.Duration }

func (cell FinalMessageSeparator) DisplayLines(ctx HistoryRenderContext) []styledLine {
	width := maxInt(12, ctx.Width)
	label := ""
	if cell.Elapsed > time.Minute {
		label = "─ Worked for " + formatElapsedCompact(cell.Elapsed) + " ─"
	}
	if label == "" {
		return []styledLine{{{Text: strings.Repeat("─", width), Style: styleSeparator}}}
	}
	line := xansi.Truncate(label+strings.Repeat("─", maxInt(0, width-lipgloss.Width(label))), width, "")
	return []styledLine{{{Text: line, Style: styleSeparator}}}
}

func (cell FinalMessageSeparator) RawLines() []string {
	if cell.Elapsed <= time.Minute {
		return nil
	}
	return []string{"Worked for " + formatElapsedCompact(cell.Elapsed)}
}

func (FinalMessageSeparator) IsStreamContinuation() bool { return false }

type PlanUpdateCell struct {
	Explanation string
	Items       []protocol.PlanItem
}

func NewPlanUpdateCell(update protocol.PlanUpdateEvent) HistoryCell {
	return PlanUpdateCell{
		Explanation: strings.TrimSpace(update.Explanation),
		Items:       append([]protocol.PlanItem(nil), update.Items...),
	}
}

func (cell PlanUpdateCell) DisplayLines(HistoryRenderContext) []styledLine {
	lines := []styledLine{{{Text: "• ", Style: styleDim}, {Text: "Updated Plan", Style: styleBold}}}
	body := make([]styledLine, 0, len(cell.Items)+1)
	if cell.Explanation != "" {
		body = append(body, styledLine{{Text: cell.Explanation, Style: styleDim}})
	}
	if len(cell.Items) == 0 {
		body = append(body, styledLine{{Text: "(no steps provided)", Style: styleDim}})
	} else {
		for _, item := range cell.Items {
			marker := "□ "
			style := styleDim
			switch item.Status {
			case "completed":
				marker = "✔ "
			case "in_progress":
				style = styleAccent
			}
			body = append(body, styledLine{{Text: marker + item.Step, Style: style}})
		}
	}
	for index, line := range body {
		prefix := "    "
		if index == 0 {
			prefix = "  └ "
		}
		lines = append(lines, append(styledLine{{Text: prefix, Style: styleDim}}, line...))
	}
	return lines
}

func (cell PlanUpdateCell) RawLines() []string {
	lines := []string{"Updated Plan"}
	if cell.Explanation != "" {
		lines = append(lines, cell.Explanation)
	}
	if len(cell.Items) == 0 {
		return append(lines, "(no steps provided)")
	}
	for _, item := range cell.Items {
		lines = append(lines, string(item.Status)+": "+item.Step)
	}
	return lines
}

func (PlanUpdateCell) IsStreamContinuation() bool { return false }

type styledHistoryCell struct{ Lines []styledLine }

func (cell styledHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	return cloneStyledLines(cell.Lines)
}

func (cell styledHistoryCell) RawLines() []string    { return rawStyledLines(cell.Lines) }
func (styledHistoryCell) IsStreamContinuation() bool { return false }

func historyLinesForMode(cell HistoryCell, mode HistoryRenderMode, ctx HistoryRenderContext) []styledLine {
	if cell == nil {
		return nil
	}
	if mode == HistoryRenderRaw {
		return styledLinesFromStrings(cell.RawLines(), stylePlain)
	}
	return cell.DisplayLines(ctx)
}

func renderHistoryCells(cells []HistoryCell, mode HistoryRenderMode, ctx HistoryRenderContext) string {
	lines := make([]styledLine, 0)
	hasVisible := false
	for _, cell := range cells {
		cellLines := historyLinesForMode(cell, mode, ctx)
		if len(cellLines) == 0 {
			continue
		}
		if hasVisible && !cell.IsStreamContinuation() {
			lines = append(lines, styledLine{})
		}
		lines = append(lines, cellLines...)
		hasVisible = true
	}
	return renderStyledLines(lines, ctx)
}

func renderStyledLines(lines []styledLine, ctx HistoryRenderContext) string {
	renderedLines := make([]string, 0, len(lines))
	for _, line := range lines {
		var rendered strings.Builder
		for _, span := range line {
			if span.Style == styleRendered {
				rendered.WriteString(span.Text)
				continue
			}
			rendered.WriteString(styleForSemantic(ctx, span.Style).Render(span.Text))
		}
		renderedLines = append(renderedLines, rendered.String())
	}
	return strings.Join(renderedLines, "\n")
}

func styleForSemantic(ctx HistoryRenderContext, style semanticStyle) lipgloss.Style {
	switch style {
	case styleDim:
		return ctx.Palette.dim()
	case styleBold:
		return ctx.Palette.bold()
	case styleAccent:
		return ctx.Palette.accent()
	case styleCommand:
		return ctx.Palette.command()
	case styleSuccess:
		return ctx.Palette.success()
	case styleFailure:
		return ctx.Palette.failure()
	case styleWarning:
		return ctx.Palette.warning()
	case styleUser:
		result := ctx.Palette.user()
		if ctx.Palette.Level == colorLevelTrueColor && !ctx.Palette.NoColor {
			result = result.Width(maxInt(12, ctx.Width))
		}
		return result
	case styleSeparator:
		return ctx.Palette.separator()
	default:
		return ctx.Palette.plain()
	}
}

func rawLines(content string) []string {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func styledLinesFromText(content string, style semanticStyle) []styledLine {
	if content == "" {
		return nil
	}
	return styledLinesFromStrings(strings.Split(content, "\n"), style)
}

func styledLinesFromStrings(lines []string, style semanticStyle) []styledLine {
	result := make([]styledLine, 0, len(lines))
	for _, line := range lines {
		result = append(result, styledLine{{Text: line, Style: style}})
	}
	return result
}

func rawStyledLines(lines []styledLine) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		var raw strings.Builder
		for _, span := range line {
			raw.WriteString(span.Text)
		}
		result = append(result, raw.String())
	}
	return result
}

func cloneStyledLines(lines []styledLine) []styledLine {
	result := make([]styledLine, len(lines))
	for index, line := range lines {
		result[index] = append(styledLine(nil), line...)
	}
	return result
}
