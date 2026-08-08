package tui

import (
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

type transcriptCellKind uint8

const (
	cellUser transcriptCellKind = iota
	cellAssistant
	cellNotice
	cellError
	cellPlan
	cellDiagnostic
	cellTool
	cellSeparator
)

type semanticStyle uint8

const (
	stylePlain semanticStyle = iota
	styleDim
	styleBold
	styleAccent
	styleSuccess
	styleFailure
	styleWarning
)

type styledSpan struct {
	Text  string
	Style semanticStyle
}
type styledLine []styledSpan

type transcriptRenderContext struct {
	Width       int
	Palette     terminalPalette
	Markdown    *glamour.TermRenderer
	Now         time.Time
	MotionStart time.Time
	Motion      motionMode
}

type transcriptCell interface {
	Kind() transcriptCellKind
	Render(transcriptRenderContext) string
	RawLines() []string
}

type activeCell interface {
	transcriptCell
	Apply(event.Event) bool
	Complete() transcriptCell
	IsComplete() bool
}

type transcriptState struct {
	Cells                      []transcriptCell
	Committed                  int
	ActiveCell                 activeCell
	NeedsFinalMessageSeparator bool
	HadWorkActivity            bool
	LastAssistantMarkdown      string
}

func (state *transcriptState) append(cells ...transcriptCell) {
	for _, cell := range cells {
		if cell != nil {
			state.Cells = append(state.Cells, cell)
		}
	}
}

func (state *transcriptState) flushActive() {
	if state.ActiveCell == nil {
		return
	}
	state.append(state.ActiveCell.Complete())
	state.ActiveCell = nil
}

func (state *transcriptState) reset() { *state = transcriptState{} }

type textTranscriptCell struct {
	kind    transcriptCellKind
	content string
}

func newTextCell(kind transcriptCellKind, content string) transcriptCell {
	return textTranscriptCell{kind: kind, content: strings.TrimSpace(content)}
}
func (cell textTranscriptCell) Kind() transcriptCellKind { return cell.kind }
func (cell textTranscriptCell) RawLines() []string {
	if cell.content == "" {
		return nil
	}
	return strings.Split(cell.content, "\n")
}
func (cell textTranscriptCell) Render(ctx transcriptRenderContext) string {
	content := sanitizeFullscreenContent(cell.content)
	width := maxInt(12, ctx.Width)
	switch cell.kind {
	case cellUser:
		wrapped := xansi.Hardwrap("› "+content, width, true)
		style := ctx.Palette.user()
		if ctx.Palette.Level == colorLevelTrueColor && !ctx.Palette.NoColor {
			style = style.Width(width)
		}
		return style.Render(wrapped)
	case cellAssistant:
		if ctx.Markdown != nil {
			if rendered, err := ctx.Markdown.Render(content); err == nil {
				return ctx.Palette.plain().Render(prefixRenderedBlock(rendered, "• "))
			}
		}
		return ctx.Palette.plain().Render(prefixRenderedBlock(content, "• "))
	case cellError:
		return ctx.Palette.failure().Render("Error: " + content)
	case cellPlan:
		return ctx.Palette.warning().Render(content)
	case cellNotice, cellDiagnostic:
		return ctx.Palette.dim().Render(content)
	default:
		return ctx.Palette.plain().Render(content)
	}
}

type finalMessageSeparatorCell struct{ elapsed time.Duration }

func (finalMessageSeparatorCell) Kind() transcriptCellKind { return cellSeparator }
func (cell finalMessageSeparatorCell) RawLines() []string {
	if cell.elapsed <= time.Minute {
		return nil
	}
	return []string{"Worked for " + formatElapsedCompact(cell.elapsed)}
}
func (cell finalMessageSeparatorCell) Render(ctx transcriptRenderContext) string {
	width := maxInt(12, ctx.Width)
	label := ""
	if cell.elapsed > time.Minute {
		label = "─ Worked for " + formatElapsedCompact(cell.elapsed) + " ─"
	}
	if label == "" {
		return ctx.Palette.separator().Render(strings.Repeat("─", width))
	}
	return ctx.Palette.separator().Render(xansi.Truncate(label+strings.Repeat("─", maxInt(0, width-lipgloss.Width(label))), width, ""))
}

type styledTranscriptCell struct {
	kind  transcriptCellKind
	lines []styledLine
}

type planUpdateCell struct {
	explanation string
	items       []event.PlanItem
}

func newPlanUpdateCell(update event.PlanUpdated) transcriptCell {
	return planUpdateCell{
		explanation: strings.TrimSpace(update.Explanation),
		items:       append([]event.PlanItem(nil), update.Items...),
	}
}

func (planUpdateCell) Kind() transcriptCellKind { return cellPlan }
func (cell planUpdateCell) RawLines() []string {
	lines := []string{"Updated Plan"}
	if cell.explanation != "" {
		lines = append(lines, cell.explanation)
	}
	if len(cell.items) == 0 {
		return append(lines, "(no steps provided)")
	}
	for _, item := range cell.items {
		lines = append(lines, string(item.Status)+": "+item.Step)
	}
	return lines
}
func (cell planUpdateCell) Render(ctx transcriptRenderContext) string {
	lines := []styledLine{{{Text: "• ", Style: styleDim}, {Text: "Updated Plan", Style: styleBold}}}
	body := make([]styledLine, 0, len(cell.items)+1)
	if cell.explanation != "" {
		body = append(body, styledLine{{Text: cell.explanation, Style: styleDim}})
	}
	if len(cell.items) == 0 {
		body = append(body, styledLine{{Text: "(no steps provided)", Style: styleDim}})
	} else {
		for _, item := range cell.items {
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
	return styledTranscriptCell{kind: cellPlan, lines: lines}.Render(ctx)
}

func (cell styledTranscriptCell) Kind() transcriptCellKind { return cell.kind }
func (cell styledTranscriptCell) RawLines() []string {
	result := make([]string, 0, len(cell.lines))
	for _, line := range cell.lines {
		var raw strings.Builder
		for _, span := range line {
			raw.WriteString(span.Text)
		}
		result = append(result, raw.String())
	}
	return result
}
func (cell styledTranscriptCell) Render(ctx transcriptRenderContext) string {
	lines := make([]string, 0, len(cell.lines))
	for _, line := range cell.lines {
		var rendered strings.Builder
		for _, span := range line {
			rendered.WriteString(styleForSemantic(ctx.Palette, span.Style).Render(span.Text))
		}
		lines = append(lines, rendered.String())
	}
	return strings.Join(lines, "\n")
}

func styleForSemantic(p terminalPalette, style semanticStyle) lipgloss.Style {
	switch style {
	case styleDim:
		return p.dim()
	case styleBold:
		return p.bold()
	case styleAccent:
		return p.accent()
	case styleSuccess:
		return p.success()
	case styleFailure:
		return p.failure()
	case styleWarning:
		return p.warning()
	default:
		return p.plain()
	}
}

func renderTranscriptCells(cells []transcriptCell, ctx transcriptRenderContext) string {
	parts := make([]string, 0, len(cells))
	for _, cell := range cells {
		if cell == nil {
			continue
		}
		if rendered := strings.TrimRight(cell.Render(ctx), "\n"); rendered != "" {
			parts = append(parts, rendered)
		}
	}
	return strings.Join(parts, "\n\n")
}
