package tui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

func (model appModel) transcriptContent() string {
	cells := append([]HistoryCell(nil), model.historyCells...)
	if model.draft != "" {
		cells = append(cells, NewAgentMessageCell(model.draft))
	}
	if model.transcript.ActiveCell != nil {
		cells = append(cells, model.transcript.ActiveCell)
	}
	return renderHistoryCells(cells, model.historyMode, model.historyRenderContext())
}

func (model *appModel) displayLinesForHistoryInsert(cell HistoryCell) []styledLine {
	if model == nil || cell == nil {
		return nil
	}
	lines := historyLinesForMode(cell, model.historyMode, model.historyRenderContext())
	if len(lines) == 0 {
		return nil
	}
	if model.hasEmittedHistoryLines && !cell.IsStreamContinuation() {
		lines = append([]styledLine{{}}, lines...)
	}
	model.hasEmittedHistoryLines = true
	return lines
}

func (model *appModel) flushHistory() tea.Cmd {
	if model == nil || len(model.pendingHistoryCells) == 0 {
		return nil
	}
	pending := append([]HistoryCell(nil), model.pendingHistoryCells...)
	model.pendingHistoryCells = nil
	var lines []styledLine
	for _, cell := range pending {
		lines = append(lines, model.displayLinesForHistoryInsert(cell)...)
	}
	output := renderStyledLines(lines, model.historyRenderContext())
	if output == "" {
		return nil
	}
	return tea.Println(output)
}

func (model appModel) renderActiveDraft() string {
	if strings.TrimSpace(model.draft) == "" {
		return ""
	}
	available := maxInt(1, model.height-lipgloss.Height(model.composerView())-2)
	sourceLines := strings.Split(model.draft, "\n")
	if len(sourceLines) > available {
		sourceLines = sourceLines[len(sourceLines)-available:]
	}
	rendered := model.renderHistoryCell(NewAgentMessageCell(strings.Join(sourceLines, "\n")))
	lines := strings.Split(rendered, "\n")
	if len(lines) > available {
		lines = lines[len(lines)-available:]
	}
	return strings.Join(lines, "\n")
}

func (model appModel) renderActiveCell() string {
	if model.transcript.ActiveCell == nil {
		return ""
	}
	rendered := model.renderHistoryCell(model.transcript.ActiveCell)
	available := maxInt(1, model.height-lipgloss.Height(model.composerView())-4)
	lines := strings.Split(rendered, "\n")
	if len(lines) > available {
		lines = lines[len(lines)-available:]
	}
	return strings.Join(lines, "\n")
}

func prefixRenderedBlock(value, prefix string) string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(xansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(xansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return strings.TrimSpace(prefix)
	}
	lines[0] = prefix + lines[0]
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
