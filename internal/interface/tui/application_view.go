package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

func (model fullscreenModel) View() (rendered string) {
	defer func() {
		if model.app != nil && model.app.options.NoColor {
			rendered = xansi.Strip(rendered)
		}
	}()
	if model.viewingDetails {
		return model.renderTranscriptViewer()
	}
	input := model.inputBox()
	status := model.statusBar()
	parts := make([]string, 0, 3)
	if active := model.renderActiveCell(); active != "" {
		parts = append(parts, active)
	}
	if draft := model.renderActiveDraft(); draft != "" {
		parts = append(parts, draft)
	}
	if working := model.workingLine(); working != "" {
		parts = append(parts, working)
	}
	activity := strings.Join(parts, "\n\n")
	inputRegion := input + "\n\n" + status
	if activity == "" {
		return "\n\n" + inputRegion
	}
	if model.hasEmittedHistoryLines {
		activity = "\n" + activity
	}
	return activity + "\n\n\n" + inputRegion
}

func newTranscriptViewport(width, height int) viewport.Model {
	viewer := viewport.New(maxInt(20, width-4), maxInt(3, height-4))
	viewer.MouseWheelEnabled = false
	return viewer
}

func (model *fullscreenModel) resizeTranscriptViewport() {
	if model == nil {
		return
	}
	model.detailViewport.Width = maxInt(20, model.width-4)
	model.detailViewport.Height = maxInt(3, model.height-4)
}

func (model *fullscreenModel) refreshTranscriptViewport() {
	if model == nil {
		return
	}
	model.resizeTranscriptViewport()
	model.detailViewport.SetContent(model.details.Render())
	model.detailViewport.GotoTop()
}

func (model fullscreenModel) handleDetailViewerKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "ctrl+t":
		model.viewingDetails = false
		return model, model.input.Focus()
	case "ctrl+c":
		model.viewingDetails = false
		return model, model.input.Focus()
	}
	var command tea.Cmd
	model.detailViewport, command = model.detailViewport.Update(key)
	return model, command
}

func (model fullscreenModel) renderTranscriptViewer() string {
	header := model.palette.strong().Render("Transcript Details") + "  " + model.palette.dim().Render("↑/↓ · PgUp/PgDn · Esc return")
	footer := model.palette.dim().Render(fmt.Sprintf("%d retained item(s) · bounded in memory", len(model.details.items)))
	return strings.Join([]string{header, model.detailViewport.View(), footer}, "\n")
}

func (model fullscreenModel) banner() (rendered string) {
	defer func() {
		if model.app != nil && model.app.options.NoColor {
			rendered = xansi.Strip(rendered)
		}
	}()
	width := maxInt(4, model.width)
	logo := model.palette.plain().Render(strings.Trim(terminalLogo(width), "\r\n"))
	version := strings.TrimSpace(model.startup.Version)
	if version != "" {
		version = " (" + version + ")"
	}
	title := model.palette.dim().Render(">_ ") + model.palette.bold().Render("Amadeus") + model.palette.dim().Render(version)
	modelName := strings.TrimSpace(model.model)
	project := strings.TrimSpace(model.startup.Project)
	rows := []string{title, ""}
	innerWidth := minInt(maxInt(0, width-4), 56)
	rowWidth := maxInt(12, innerWidth-2)
	if modelName != "" {
		rows = append(rows, bannerMetadataRow("model:", modelName, rowWidth))
	}
	if project != "" {
		rows = append(rows, bannerMetadataRow("directory:", project, rowWidth))
	}
	panelWidth := maxInt(4, innerWidth)
	panel := fullscreenPanelStyle.BorderForeground(model.palette.border().GetForeground()).Width(panelWidth).Render(strings.Join(rows, "\n"))
	return logo + "\n\n" + panel
}

func bannerMetadataRow(label, value string, width int) string {
	const labelWidth = 11
	if width <= labelWidth {
		return truncateFullscreen(strings.TrimSpace(label)+strings.TrimSpace(value), width)
	}
	return fmt.Sprintf("%-*s%s", labelWidth, label, truncateFullscreen(strings.TrimSpace(value), width-labelWidth))
}

func isTerminalControlResponse(message tea.KeyMsg) bool {
	value := message.String()
	if len(message.Runes) > 0 {
		value += string(message.Runes)
	}
	return terminalMouseResponseRE.MatchString(value) ||
		strings.Contains(value, "]11;") ||
		strings.Contains(value, "]10;") ||
		strings.Contains(value, "]12;") ||
		strings.Contains(value, "rgb:") ||
		strings.Contains(value, "\x1b]") ||
		strings.ContainsRune(value, '\u009d')
}

func (model fullscreenModel) isRecentMouseControlFragment(message tea.KeyMsg) bool {
	if model.lastMouseEvent.IsZero() || time.Since(model.lastMouseEvent) > terminalControlFragmentWindow {
		return false
	}
	value := message.String()
	if len(message.Runes) > 0 {
		value = string(message.Runes)
	}
	return value != "" && strings.Trim(value, "[") == ""
}

func (model *fullscreenModel) sanitizeInput() {
	value := model.input.Value()
	clean := stripTerminalControlResponses(value)
	if clean != value {
		model.input.SetValue(clean)
		model.input.CursorEnd()
	}
}

var (
	terminalControlResponseRE = regexp.MustCompile(`(?s)(?:\x1b\]|\])?(?:10|11|12);rgb:[0-9a-fA-F]{1,4}/[0-9a-fA-F]{1,4}/[0-9a-fA-F]{1,4}(?:\x1b\\|\\)?`)
	terminalMouseResponseRE   = regexp.MustCompile(`(?:\x1b\[|\x{009b}|\[)?<\d{1,3};\d{1,4};\d{1,4}[mM]`)
)

const terminalControlFragmentWindow = 300 * time.Millisecond

func stripTerminalControlResponses(value string) string {
	value = terminalControlResponseRE.ReplaceAllString(value, "")
	value = terminalMouseResponseRE.ReplaceAllString(value, "")
	value = strings.ReplaceAll(value, "]11;", "")
	value = strings.ReplaceAll(value, "]10;", "")
	value = strings.ReplaceAll(value, "]12;", "")
	return strings.TrimLeft(value, "\x1b\\] ")
}

func (model fullscreenModel) inputBox() string {
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
	input := fullscreenInputFillStyle.Width(width).Render(strings.TrimRight(model.input.View(), "\n"))
	if model.slashPopup.active() {
		visible, start := model.slashPopup.visibleItems()
		items := make([]listVisualItem, 0, len(visible))
		for index, command := range visible {
			items = append(items, listVisualItem{
				Name: "/" + command.Name(), Description: command.Description(),
				Selected: start+index == model.slashPopup.selected,
			})
		}
		list := model.renderListVisual(listVisual{
			Items: items,
		}, width)
		return input + "\n\n" + list
	}
	return input
}

func (model fullscreenModel) statusBar() string {
	width := maxInt(40, model.width)
	parts := []statusBarPart{}
	if modelName := strings.TrimSpace(model.model); modelName != "" {
		parts = append(parts, statusBarPart{text: modelName, style: model.palette.statusLineStyle(statusAccentModel)})
	} else {
		parts = append(parts, statusBarPart{text: "AMADEUS", style: model.palette.statusLineStyle(statusAccentModel)})
	}
	if title := strings.TrimSpace(model.sessionTitle); title != "" && title != "draft" {
		parts = append(parts, statusBarPart{text: title, style: model.palette.dim()})
	}
	if project := strings.TrimSpace(model.startup.Project); project != "" {
		parts = append(parts, statusBarPart{text: project, style: model.palette.statusLineStyle(statusAccentPath)})
	}
	if branch := strings.TrimSpace(model.startup.Branch); branch != "" {
		parts = append(parts, statusBarPart{text: branch, style: model.palette.statusLineStyle(statusAccentBranch)})
	}
	if model.collaboration == turn.ModeKindPlan {
		parts = append(parts, statusBarPart{text: "Plan", style: model.palette.statusLineStyle(statusAccentMode)})
	}
	contextWindow := model.contextLimit
	if contextWindow <= 0 {
		contextWindow = model.startup.ContextWindow
	}
	if contextWindow > 0 {
		percent := int64(0)
		if model.contextUsage > 0 {
			percent = minInt64(100, model.contextUsage*100/contextWindow)
		}
		contextStyle := statusContextStyle(model.palette, percent)
		parts = append(parts,
			statusBarPart{text: fmt.Sprintf("Context %d%% used", percent), style: contextStyle},
			statusBarPart{text: compactTokenCount(contextWindow) + " window", style: model.palette.statusLineStyle(statusAccentUsage)},
		)
	}
	if len(parts) == 1 && strings.TrimSpace(model.startup.Project) == "" && strings.TrimSpace(model.startup.Branch) == "" && contextWindow <= 0 {
		parts = append(parts, statusBarPart{text: model.status, style: model.palette.dim()})
	}
	line := renderStatusBarParts(parts, model.palette)
	if lipgloss.Width(line) > width {
		for len(parts) > 1 && lipgloss.Width(renderStatusBarParts(parts, model.palette)) > width {
			parts = append(parts[:1], parts[2:]...)
		}
		line = xansi.Truncate(renderStatusBarParts(parts, model.palette), width, "")
	}
	return line
}

func statusContextStyle(palette terminalPalette, percent int64) lipgloss.Style {
	switch {
	case percent >= 90:
		return palette.failure()
	case percent >= 70:
		return palette.warning()
	default:
		return palette.statusLineStyle(statusAccentUsage)
	}
}

type statusBarPart struct {
	text  string
	style lipgloss.Style
}

func renderStatusBarParts(parts []statusBarPart, palette terminalPalette) string {
	var builder strings.Builder
	separator := palette.dim().Render(" · ")
	for index, part := range parts {
		if index > 0 {
			builder.WriteString(separator)
		}
		builder.WriteString(part.style.Render(part.text))
	}
	return builder.String()
}

func (model fullscreenModel) workingLine() string {
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

func (model fullscreenModel) runElapsed() time.Duration {
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

func (model *fullscreenModel) updateInputLayout() {
	width := maxInt(40, model.width)
	model.input.SetWidth(width - 2)
	rows := 1 + strings.Count(model.input.Value(), "\n")
	if rows > fullscreenMaxInputRows {
		rows = fullscreenMaxInputRows
	}
	model.input.SetHeight(rows)
}

func (model fullscreenModel) transcriptContent() string {
	cells := append([]HistoryCell(nil), model.historyCells...)
	if model.draft != "" {
		cells = append(cells, NewAgentMessageCell(model.draft))
	}
	if model.transcript.ActiveCell != nil {
		cells = append(cells, model.transcript.ActiveCell)
	}
	return renderHistoryCells(cells, model.historyMode, model.historyRenderContext())
}

func (model *fullscreenModel) displayLinesForHistoryInsert(cell HistoryCell) []styledLine {
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

func (model *fullscreenModel) flushHistory() tea.Cmd {
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

func (model fullscreenModel) renderActiveDraft() string {
	if strings.TrimSpace(model.draft) == "" {
		return ""
	}
	available := maxInt(1, model.height-lipgloss.Height(model.inputBox())-lipgloss.Height(model.statusBar())-2)
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

func (model fullscreenModel) renderActiveCell() string {
	if model.transcript.ActiveCell == nil {
		return ""
	}
	rendered := model.renderHistoryCell(model.transcript.ActiveCell)
	available := maxInt(1, model.height-lipgloss.Height(model.inputBox())-lipgloss.Height(model.statusBar())-4)
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

func sanitizeFullscreenContent(value string) string {
	value = xansi.Strip(value)
	var builder strings.Builder
	for _, character := range value {
		if character == '\n' || character == '\t' || !unicode.IsControl(character) {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func truncateFullscreen(value string, width int) string {
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
