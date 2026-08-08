package tui

import (
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/policy"
	tea "github.com/charmbracelet/bubbletea"
)

func (model fullscreenModel) resolveApproval(decision policy.ApprovalDecision) (tea.Model, tea.Cmd) {
	prompt := model.approval
	model.approval = nil
	model.selection = nil
	model.selectionKind = ""
	model.status = "executing"
	prompt.response <- fullscreenApprovalResult{decision: decision}
	commands := []tea.Cmd{model.input.Focus()}
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
			return model, func() tea.Msg {
				message, err := model.app.options.Rename(model.ctx, value)
				return fullscreenRenameMsg{message: message, err: err}
			}
		default:
			if len(key.Runes) > 0 && key.Alt == false {
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
		if model.selectionKind == "approval" && model.approval != nil {
			choices := approvalChoices(model.approval.request)
			return model.resolveApproval(choices[len(choices)-1].decision)
		}
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
		case "approval":
			choices := approvalChoices(model.approval.request)
			if selected >= 0 && selected < len(choices) {
				return model.resolveApproval(choices[selected].decision)
			}
		case "resume":
			if selected >= 0 && selected < len(model.sessions) {
				model.status = "resuming session"
				id := model.sessions[selected].ID
				return model, func() tea.Msg {
					message, err := model.app.options.Resume(model.ctx, id)
					return fullscreenResumeMsg{message: message, err: err}
				}
			}
		case "delete":
			if selected == 0 {
				model.selection = nil
				model.selectionKind = ""
				return model, model.input.Focus()
			}
			model.status = "deleting session"
			return model, func() tea.Msg {
				message, err := model.app.options.Delete(model.ctx)
				return fullscreenDeleteMsg{message: message, err: err}
			}
		case "skills-menu":
			if selected == 0 {
				model.selection = nil
				model.selectionKind = ""
				model.status = "loading skills"
				return model, func() tea.Msg {
					skills, err := model.app.options.Skills(model.ctx)
					if err != nil {
						return fullscreenSkillsMsg{err: err}
					}
					var builder strings.Builder
					for index, skill := range skills {
						if index > 0 {
							builder.WriteByte('\n')
						}
						fmt.Fprintf(&builder, "%s  %s  %s", skill.Name, skill.Source, skill.Description)
					}
					return fullscreenCommandDoneMsg{command: "/skills", output: builder.String()}
				}
			}
			model.status = "loading skills"
			return model, func() tea.Msg {
				skills, err := model.app.options.Skills(model.ctx)
				return fullscreenSkillsMsg{skills: skills, err: err}
			}
		case "skills":
			if selected >= 0 && selected < len(model.skills) && model.app.options.SetSkill != nil {
				skill := model.skills[selected]
				return model, func() tea.Msg {
					enabled := !skill.Enabled
					err := model.app.options.SetSkill(model.ctx, skill.Name, enabled)
					return fullscreenSkillSetMsg{name: skill.Name, enabled: enabled, err: err}
				}
			}
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
	lines := []string{fullscreenResumeAccentStyle.Render(overlay.Title) + "  " + fullscreenMutedStyle.Render(hint)}
	if overlay.Subtitle != "" {
		lines = append(lines, fullscreenMutedStyle.Render(overlay.Subtitle))
	}
	if model.selectionKind == "approval" && model.approval != nil {
		request := model.approval.request
		lines = append(lines,
			"Tool: "+sanitizeInlineEventText(request.ToolName),
			fmt.Sprintf("Risk: %s", request.Risk),
			"Reason: "+sanitizeInlineEventText(request.Reason),
		)
	}
	if overlay.Input || overlay.Search {
		prefix := "> "
		if overlay.Search {
			prefix = "Search: "
		}
		lines = append(lines, prefix+overlay.Value)
	}
	indices := overlay.filteredIndices()
	if len(indices) == 0 && !overlay.Input {
		lines = append(lines, fullscreenMutedStyle.Render("No matching items"))
	}
	for visibleIndex, itemIndex := range indices {
		item := overlay.Items[itemIndex]
		prefix := "  "
		if visibleIndex == overlay.Selected {
			prefix = "› "
		}
		line := prefix + item.Name
		if item.Description != "" {
			line += "  " + item.Description
		}
		if item.Disabled {
			reason := strings.TrimSpace(item.DisabledReason)
			if reason == "" {
				reason = "disabled"
			}
			line += "  (" + reason + ")"
		}
		line = truncateFullscreen(line, width)
		if visibleIndex == overlay.Selected && !item.Disabled {
			line = fullscreenResumeAccentStyle.Render(line)
		} else if item.Disabled {
			line = fullscreenMutedStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
