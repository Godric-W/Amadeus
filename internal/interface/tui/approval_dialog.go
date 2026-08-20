package tui

import (
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/policy"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type approvalDialog struct {
	choices  []fullscreenApprovalChoice
	selected int
	diffTop  int
}

func newApprovalDialog(request policy.ApprovalRequest) *approvalDialog {
	return &approvalDialog{choices: approvalChoices(request)}
}

func (dialog *approvalDialog) move(delta int) {
	if dialog == nil || len(dialog.choices) == 0 {
		return
	}
	dialog.selected = (dialog.selected + delta + len(dialog.choices)) % len(dialog.choices)
}

func (dialog *approvalDialog) scroll(delta, viewportHeight, lineCount int) {
	if dialog == nil || viewportHeight <= 0 || lineCount <= viewportHeight {
		dialog.diffTop = 0
		return
	}
	dialog.diffTop += delta
	maximum := lineCount - viewportHeight
	if dialog.diffTop < 0 {
		dialog.diffTop = 0
	}
	if dialog.diffTop > maximum {
		dialog.diffTop = maximum
	}
}

func (model fullscreenModel) handleApprovalKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if model.approval == nil || model.approvalDialog == nil {
		return model, nil
	}
	lines := approvalDiffLines(model.approval.request)
	viewportHeight := model.approvalDiffHeight()
	switch key.String() {
	case "esc", "ctrl+c":
		choices := model.approvalDialog.choices
		return model.resolveApproval(choices[len(choices)-1].decision)
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		index := int(key.String()[0] - '1')
		if index < len(model.approvalDialog.choices) {
			return model.resolveApproval(model.approvalDialog.choices[index].decision)
		}
	case "up", "k":
		model.approvalDialog.move(-1)
	case "down", "j":
		model.approvalDialog.move(1)
	case "pgup", "ctrl+u":
		model.approvalDialog.scroll(-viewportHeight, viewportHeight, len(lines))
	case "pgdown", "ctrl+d":
		model.approvalDialog.scroll(viewportHeight, viewportHeight, len(lines))
	case "enter":
		return model.resolveApproval(model.approvalDialog.choices[model.approvalDialog.selected].decision)
	}
	return model, nil
}

func (model fullscreenModel) approvalDiffHeight() int {
	height := model.height - 14
	if height < 3 {
		return 3
	}
	if height > 16 {
		return 16
	}
	return height
}

func approvalDiffLines(request policy.ApprovalRequest) []string {
	if request.Diff == nil {
		return nil
	}
	lines := make([]string, 0)
	for _, hunk := range request.Diff.Hunks {
		lines = append(lines, hunk.Lines...)
	}
	return lines
}

func (model fullscreenModel) renderApprovalDialog(width int) string {
	request := model.approval.request
	dialog := model.approvalDialog
	contentWidth := maxInt(24, width-4)
	title := strings.TrimSpace(request.Presentation.Title)
	if title == "" {
		title = "Tool use"
	}
	rows := []string{
		model.palette.separator().Render(strings.Repeat("─", maxInt(1, width))),
		" " + model.palette.strong().Render(title),
	}
	if request.Purpose == policy.ApprovalPurposeCommand {
		rows = append(rows, "")
		command := strings.TrimSpace(request.Command)
		if command == "" {
			command = approvalDetailValue(request.Presentation.Details, "Command:")
		}
		for _, line := range strings.Split(command, "\n") {
			rows = append(rows, "   "+model.palette.plain().Render(truncateFullscreen(line, maxInt(12, contentWidth-3))))
		}
		if description := approvalDetailValue(request.Presentation.Details, "Description:"); description != "" {
			rows = append(rows, "   "+model.palette.dim().Render(truncateFullscreen(description, maxInt(12, contentWidth-3))))
		}
		rows = append(rows, "", " "+model.palette.plain().Render("This command requires approval"))
	} else {
		if len(request.Presentation.Details) > 0 {
			rows = append(rows, "")
		}
		for _, detail := range request.Presentation.Details {
			rows = append(rows, "   "+model.palette.dim().Render(truncateFullscreen(detail, maxInt(12, contentWidth-3))))
		}
	}
	question := strings.TrimSpace(request.Presentation.Question)
	if question == "" {
		question = "Do you want to proceed?"
	}
	rows = append(rows, "", " "+model.palette.dim().Render(question))
	lines := approvalDiffLines(request)
	if len(lines) > 0 {
		viewportHeight := model.approvalDiffHeight()
		dialog.scroll(0, viewportHeight, len(lines))
		end := minInt(len(lines), dialog.diffTop+viewportHeight)
		visible := make([]string, 0, end-dialog.diffTop)
		for _, line := range lines[dialog.diffTop:end] {
			style := model.palette.plain()
			switch {
			case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
			case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
			case strings.HasPrefix(line, "@@"):
				style = model.palette.accent()
			}
			visible = append(visible, style.Render(truncateFullscreen(line, contentWidth)))
		}
		rows = append(rows, "", strings.Join(visible, "\n"))
		if len(lines) > viewportHeight {
			rows = append(rows, model.palette.dim().Render(fmt.Sprintf("Diff lines %d-%d of %d · PgUp/PgDn scroll", dialog.diffTop+1, end, len(lines))))
		}
	}
	rows = append(rows, "")
	for index, choice := range dialog.choices {
		prefix := "   "
		style := model.palette.plain()
		if index == dialog.selected {
			style = model.palette.selection()
		}
		rows = append(rows, prefix+style.Render(fmt.Sprintf("%d. %s", index+1, choice.label)))
	}
	rows = append(rows, "", " "+model.palette.dim().Render("Esc to reject"))
	return strings.Join(rows, "\n")
}

func approvalDetailValue(details []string, prefix string) string {
	for _, detail := range details {
		if strings.HasPrefix(detail, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(detail, prefix))
		}
	}
	return ""
}
