package tui

import (
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/policy"
	tea "github.com/charmbracelet/bubbletea"
)

func (model fullscreenModel) resolveApproval(decision policy.ApprovalDecision) (tea.Model, tea.Cmd) {
	prompt := model.approval
	model.approval = nil
	model.approvalDialog = nil
	if prompt != nil {
		if cell, ok := model.transcript.ActiveCell.(*ToolHistoryCell); ok && cell.SetApprovalState(prompt.request.ID, false) {
			model.transcript.bumpActiveCellRevision()
		}
	}
	model.selection = nil
	model.selectionKind = ""
	model.status = "working"
	commands := []tea.Cmd{model.input.Focus()}
	if prompt != nil {
		requestID := prompt.requestID
		commands = append(commands, func() tea.Msg {
			if err := model.app.options.Application.ResolveApproval(model.ctx, requestID, decision); err != nil {
				return fullscreenOperationFailedMsg{operation: "resolve approval", err: err}
			}
			return nil
		})
	}
	if model.running {
		commands = append(commands, fullscreenWorkingTick())
	}
	return model, tea.Batch(commands...)
}

func (model fullscreenModel) handleSelectionKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if model.selection == nil {
		return model, nil
	}
	if model.selection.Input {
		switch key.String() {
		case "esc", "ctrl+c":
			model.selection = nil
			model.selectionKind = ""
			return model, model.input.Focus()
		case "backspace":
			runes := []rune(model.selection.Value)
			if len(runes) > 0 {
				model.selection.Value = string(runes[:len(runes)-1])
			}
			return model, nil
		case "enter":
			value := strings.TrimSpace(model.selection.Value)
			if value == "" {
				return model, nil
			}
			model.status = "renaming session"
			return model, model.rename(value)
		default:
			if len(key.Runes) > 0 && !key.Alt {
				model.selection.Value += string(key.Runes)
			}
			return model, nil
		}
	}
	if model.selection.Search {
		switch key.String() {
		case "backspace":
			runes := []rune(model.selection.Value)
			if len(runes) > 0 {
				model.selection.Value = string(runes[:len(runes)-1])
				model.selection.Selected = 0
			}
			return model, nil
		default:
			if len(key.Runes) > 0 && !key.Alt {
				model.selection.Value += string(key.Runes)
				model.selection.Selected = 0
				return model, nil
			}
		}
	}
	switch key.String() {
	case "esc", "ctrl+c":
		model.selection = nil
		model.selectionKind = ""
		model.sessions = nil
		return model, model.input.Focus()
	case "up", "k":
		model.selection.move(-1)
		return model, nil
	case "down", "j":
		model.selection.move(1)
		return model, nil
	case "enter":
		selected, selectedOK := model.selection.selectedIndex()
		if !selectedOK {
			return model, nil
		}
		switch model.selectionKind {
		case "resume":
			if selected >= 0 && selected < len(model.sessions) {
				model.status = "resuming session"
				id := model.sessions[selected].ID
				return model, func() tea.Msg {
					model.app.options.Application.Resume(model.ctx, id)
					return nil
				}
			}
		case "delete":
			if selected == 0 {
				model.selection = nil
				model.selectionKind = ""
				return model, model.input.Focus()
			}
			model.status = "deleting session"
			generation := model.generation
			return model, func() tea.Msg {
				model.app.options.Application.Delete(model.ctx, generation)
				return nil
			}
		case "skills-menu":
			model.selection = nil
			model.selectionKind = ""
			model.status = "loading skills"
			if selected == 0 {
				model.pendingSkillsView = "list"
			} else {
				model.pendingSkillsView = "manage"
			}
			return model, func() tea.Msg {
				model.app.options.Application.LoadSkills()
				return nil
			}
		case "skills-list":
			if selected >= 0 && selected < len(model.skills) {
				skill := model.skills[selected]
				model.input.SetValue("$" + skill.Name + " ")
				model.input.CursorEnd()
				model.selection = nil
				model.selectionKind = ""
				model.updateInputLayout()
				return model, model.input.Focus()
			}
		case "skills-manage":
			if selected >= 0 && selected < len(model.skills) {
				skill := model.skills[selected]
				enabled := !skill.Enabled
				return model, func() tea.Msg {
					model.app.options.Application.SetSkillEnabled(skill.Path, enabled)
					return nil
				}
			}
		case "implement-plan":
			model.selection = nil
			model.selectionKind = ""
			if selected != 0 {
				return model, model.input.Focus()
			}
			model.collaboration = turn.ModeKindDefault
			model.running = true
			model.status = "working"
			return model, tea.Batch(model.submitTask(TaskSubmission{Content: "Implement the plan.", Mode: turn.ModeKindDefault}), model.workingTick())
		}
	}
	return model, nil
}

func (model fullscreenModel) renderSelectionOverlay(width int) string {
	overlay := model.selection
	if overlay == nil {
		return ""
	}
	hint := overlay.Hint
	if hint == "" {
		hint = "↑/↓ select · Enter confirm · Esc cancel"
	}
	visual := listVisual{Title: overlay.Title, Subtitle: overlay.Subtitle, Hint: hint}
	if overlay.Input || overlay.Search {
		visual.InputLabel = "› "
		if overlay.Search {
			visual.InputLabel = "Search: "
		}
		visual.InputValue = overlay.Value
	}
	filtered := overlay.filteredIndices()
	selectedSource := -1
	if overlay.Selected >= 0 && overlay.Selected < len(filtered) {
		selectedSource = filtered[overlay.Selected]
	}
	for _, itemIndex := range overlay.visibleIndices() {
		item := overlay.Items[itemIndex]
		visual.Items = append(visual.Items, listVisualItem{
			Name: item.Name, Description: item.Description, Disabled: item.Disabled,
			DisabledReason: item.DisabledReason, Selected: itemIndex == selectedSource,
		})
	}
	if len(filtered) == 0 && !overlay.Input {
		visual.EmptyText = "No matching items"
	}
	return model.renderListVisual(visual, width)
}
