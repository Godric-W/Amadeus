package tui

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
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
	var previous HistoryCell
	for _, cell := range cells {
		cellLines := historyLinesForMode(cell, mode, ctx)
		if len(cellLines) == 0 {
			continue
		}
		if hasVisible {
			lines = append(lines, make([]styledLine, historyBoundaryBlankRows(previous, cell))...)
		}
		lines = append(lines, cellLines...)
		hasVisible = true
		previous = cell
	}
	return renderStyledLines(lines, ctx)
}

func renderStyledLines(lines []styledLine, ctx HistoryRenderContext) string {
	renderedLines := make([]string, 0, len(lines))
	for _, line := range lines {
		var rendered strings.Builder
		for _, span := range line {
			style := styleForSemantic(ctx, span.Style)
			if span.Markdown.Bold {
				style = style.Bold(true)
			}
			if span.Markdown.Italic {
				style = style.Italic(true)
			}
			if span.Markdown.Strikethrough {
				style = style.Strikethrough(true)
			}
			if span.Markdown.Underline {
				style = style.Underline(true)
			}
			if span.Syntax != chroma.EOFType && !ctx.Palette.NoColor && ctx.Palette.Level != colorLevelNone {
				entry := markdownSyntaxStyle(ctx.Palette).Get(span.Syntax)
				if entry.Colour.IsSet() {
					style = style.Foreground(lipgloss.Color(entry.Colour.String()))
				}
				if entry.Bold == chroma.Yes {
					style = style.Bold(true)
				}
				if entry.Italic == chroma.Yes {
					style = style.Italic(true)
				}
			}
			text := style.Render(sanitizeContent(span.Text))
			if ctx.Hyperlinks {
				text = osc8WebHyperlink(span.Destination, text)
			}
			rendered.WriteString(text)
		}
		renderedLines = append(renderedLines, rendered.String())
	}
	return strings.Join(renderedLines, "\n")
}

func markdownSyntaxStyle(palette terminalPalette) *chroma.Style {
	if palette.Dark {
		return chromastyles.Get("dracula")
	}
	return chromastyles.Get("github")
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
	case styleStrike:
		return ctx.Palette.plain().Strikethrough(true)
	case styleQuote:
		return ctx.Palette.quote()
	case styleOrderedListMarker:
		return ctx.Palette.orderedListMarker()
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
