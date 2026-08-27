package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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
		return ctx.Palette.turnSeparator()
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
