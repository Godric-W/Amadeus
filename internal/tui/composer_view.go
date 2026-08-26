package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

func (model appModel) inputBox() string {
	width := maxInt(40, model.width)
	if model.approvalDialog != nil && model.approval != nil {
		return model.renderApprovalDialog(width)
	}
	if model.userInputDialog != nil && model.userInputRequest != nil {
		return model.renderRequestUserInputDialog(width)
	}
	if model.selection != nil {
		return model.renderSelectionOverlay(width)
	}
	return inputFillStyle.Width(width).Render(renderTextareaWindow(model.input))
}

func (model appModel) composerView() string {
	input := model.inputBox()
	auxiliary := model.composerAuxiliaryView()
	if auxiliary == "" {
		return input
	}
	return input + "\n\n" + auxiliary
}

func (model appModel) composerAuxiliaryView() string {
	if model.selection != nil || model.approvalDialog != nil || model.userInputDialog != nil {
		return ""
	}
	if model.slashPopup.active() {
		return model.slashPopupView()
	}
	preview := model.queuedInputPreview()
	footer := model.footerView()
	if preview == "" {
		return footer
	}
	if footer == "" {
		return preview
	}
	return preview + "\n" + footer
}

func (model appModel) slashPopupView() string {
	visible, start := model.slashPopup.visibleItems()
	items := make([]listVisualItem, 0, len(visible))
	for index, command := range visible {
		items = append(items, listVisualItem{Name: "/" + command.Name(), Description: command.Description(), Selected: start+index == model.slashPopup.selected})
	}
	rendered := model.renderListVisual(listVisual{Items: items, HideSelectionMarker: true}, maxInt(40, model.width))
	missingRows := model.slashPopup.rows - len(visible)
	if missingRows <= 0 {
		return rendered
	}
	padding := strings.Repeat(" \n", missingRows)
	return rendered + "\n" + strings.TrimSuffix(padding, "\n")
}

func (model appModel) workingLine() string {
	if (!model.running && !model.retryStatus.active) || model.approval != nil {
		return ""
	}
	now := time.Now()
	if model.clock != nil {
		now = model.clock.Now()
	}
	elapsed := elapsedRunDurationAt(model.runStartedAt, now)
	marker := spinnerGlyph(now, model.motionStartedAt, model.motion, model.palette)
	word := shimmerText(statusHeader(model.status), now, model.motionStartedAt, model.motion, model.palette)
	line := marker + word + model.palette.dim().Render(fmt.Sprintf(" (%s • esc to interrupt)", formatElapsedCompact(elapsed)))
	line = xansi.Truncate(line, maxInt(12, model.width), "")
	if details := strings.TrimSpace(model.statusDetails); details != "" {
		detailLine := model.palette.dim().Render("  └ " + xansi.Truncate(details, maxInt(8, model.width-4), ""))
		return line + "\n" + detailLine
	}
	return line
}

func (model appModel) runElapsed() time.Duration {
	now := time.Now()
	if model.clock != nil {
		now = model.clock.Now()
	}
	return elapsedRunDurationAt(model.runStartedAt, now)
}

func elapsedRunDurationAt(startedAt, now time.Time) time.Duration {
	if startedAt.IsZero() {
		return 0
	}
	elapsed := now.Sub(startedAt).Round(time.Second)
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func compactTokenCount(value int64) string {
	switch {
	case value >= 1_000_000:
		if value%1_000_000 == 0 {
			return fmt.Sprintf("%dM", value/1_000_000)
		}
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%dK", value/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func (model *appModel) updateInputLayout() {
	width := maxInt(40, model.width)
	model.input.SetWidth(width)
	rows, _, _ := textareaVisualMetrics(model.input)
	rows = minInt(maxInputRows, maxInt(1, rows))
	model.input.SetHeight(rows)
}

func textareaVisualMetrics(input textarea.Model) (rows, cursorRow, cursorColumn int) {
	logicalLines := strings.Split(input.Value(), "\n")
	currentLine := minInt(maxInt(0, input.Line()), len(logicalLines)-1)
	info := input.LineInfo()
	rowsBefore := 0
	for index, line := range logicalLines {
		probe := input
		probe.SetValue(line)
		probe.CursorEnd()
		height := maxInt(1, probe.LineInfo().Height)
		if index < currentLine {
			rowsBefore += height
		}
		rows += height
	}
	cursorRow = rowsBefore + info.RowOffset
	cursorColumn = lipgloss.Width(inputPrompt) + info.CharOffset
	return rows, cursorRow, cursorColumn
}

func renderTextareaWindow(input textarea.Model) string {
	rows, cursorRow, _ := textareaVisualMetrics(input)
	rows = maxInt(1, rows)
	visibleRows := minInt(maxInputRows, rows)
	renderInput := input
	renderInput.MaxHeight = 0
	renderInput.SetHeight(rows)
	lines := strings.Split(strings.TrimRight(renderInput.View(), "\n"), "\n")
	if len(lines) <= visibleRows {
		return strings.Join(lines, "\n")
	}
	maximumStart := len(lines) - visibleRows
	start := minInt(maxInt(0, cursorRow-visibleRows+1), maximumStart)
	return strings.Join(lines[start:start+visibleRows], "\n")
}
