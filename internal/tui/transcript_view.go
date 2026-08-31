package tui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

const clearScrollback = "\x1b[3J"

func (model appModel) transcriptCells() []HistoryCell {
	return model.TranscriptSurface.unprintedCells(model.transcript.ActiveCell)
}

func (model appModel) transcriptContent(height int) string {
	return model.TranscriptSurface.render(model.transcriptCells(), model.historyMode, model.historyRenderContext(), height)
}

func (model appModel) transcriptViewportHeight(composer, working string) int {
	if len(model.transcriptCells()) == 0 {
		return 0
	}
	height := model.height - lipgloss.Height(composer) - 2
	if working != "" {
		height -= lipgloss.Height(working) + 2
	}
	return maxInt(0, height)
}

func (model *appModel) scrollTranscriptPage(direction int) bool {
	if model == nil || direction == 0 {
		return false
	}
	composer := model.composerView()
	working := model.workingLine()
	height := model.transcriptViewportHeight(composer, working)
	if height <= 0 {
		return false
	}
	return model.TranscriptSurface.scroll(
		model.transcriptCells(),
		model.historyMode,
		model.historyRenderContext(),
		height,
		direction*height,
	)
}

func (model *appModel) flushHistory() tea.Cmd {
	if model == nil || model.transcriptReflow.pending {
		return nil
	}
	prints := model.TranscriptSurface.takePrintableCells()
	commands := make([]tea.Cmd, 0, len(prints))
	ctx := model.historyRenderContext()
	for _, printCell := range prints {
		lines := historyLinesForMode(printCell.cell, model.historyMode, ctx)
		if printCell.leadingBlankRows > 0 {
			lines = append(make([]styledLine, printCell.leadingBlankRows), lines...)
		}
		output := boundHistoryPrintWidth(renderStyledLines(lines, ctx), model.width)
		if output != "" {
			commands = append(commands, tea.Println(output))
		}
	}
	return tea.Sequence(commands...)
}

// reflowNativeHistory replaces terminal-owned wrapped rows with a render from
// the source-backed immutable prefix. Bubble Tea writes this payload through
// its renderer, preserving the history insertion order while the clear
// sequence removes stale scrollback rows.
func (model *appModel) reflowNativeHistory() tea.Cmd {
	if model == nil || !model.TranscriptSurface.printedVisible {
		return nil
	}
	cells := model.TranscriptSurface.reflowCells()
	rendered := renderHistoryCells(cells, model.historyMode, model.historyRenderContext())
	rendered = boundHistoryPrintWidth(rendered, model.width)
	model.TranscriptSurface.markReflowed()
	return tea.Sequence(
		func() tea.Msg { return tea.ClearScreen() },
		tea.Println(clearScrollback+rendered),
	)
}

func boundHistoryPrintWidth(rendered string, width int) string {
	if rendered == "" || width <= 0 {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	for index := range lines {
		lines[index] = xansi.Truncate(lines[index], width, "")
	}
	return strings.Join(lines, "\n")
}

func sanitizeContent(value string) string {
	value = xansi.Strip(value)
	var builder strings.Builder
	for _, character := range value {
		if character == '\n' || character == '\t' || !unicode.IsControl(character) {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func truncateLine(value string, width int) string {
	if width <= 0 || lipgloss.Width(value) <= width {
		return value
	}
	return xansi.Truncate(value, width, "…")
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
