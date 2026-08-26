package tui

import (
	"context"
	"strings"

	"github.com/Godric-W/Amadeus/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type requestUserInputDialog struct {
	question int
	selected int
	chosen   map[string][]string
	other    bool
	text     string
}

func newRequestUserInputDialog(protocol.RequestUserInputEvent) *requestUserInputDialog {
	return &requestUserInputDialog{chosen: make(map[string][]string)}
}

func (model appModel) handleRequestUserInputKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	request, dialog := model.userInputRequest, model.userInputDialog
	if request == nil || dialog == nil || dialog.question >= len(request.Questions) {
		return model, nil
	}
	question := request.Questions[dialog.question]
	if dialog.other {
		switch key.String() {
		case "esc":
			dialog.other, dialog.text = false, ""
		case "backspace":
			runes := []rune(dialog.text)
			if len(runes) > 0 {
				dialog.text = string(runes[:len(runes)-1])
			}
		case "enter":
			if strings.TrimSpace(dialog.text) != "" {
				dialog.chosen[question.ID] = []string{strings.TrimSpace(dialog.text)}
				return model.advanceUserInputDialog()
			}
		default:
			if len(key.Runes) > 0 {
				dialog.text += string(key.Runes)
			}
		}
		return model, nil
	}
	count := len(question.Options) + 1
	switch key.String() {
	case "esc", "ctrl+c":
		model.userInputRequest, model.userInputDialog = nil, nil
		model.status = "cancelling"
		return model, model.interrupt()
	case "up", "k":
		dialog.selected = (dialog.selected - 1 + count) % count
	case "down", "j":
		dialog.selected = (dialog.selected + 1) % count
	case " ":
		if question.MultiSelect && dialog.selected < len(question.Options) {
			dialog.toggle(question.ID, question.Options[dialog.selected].Label)
		}
	case "enter":
		if dialog.selected == len(question.Options) {
			dialog.other = true
			return model, nil
		}
		label := question.Options[dialog.selected].Label
		if question.MultiSelect {
			dialog.toggle(question.ID, label)
			if len(dialog.chosen[question.ID]) == 0 {
				return model, nil
			}
		} else {
			dialog.chosen[question.ID] = []string{label}
		}
		return model.advanceUserInputDialog()
	}
	return model, nil
}

func (dialog *requestUserInputDialog) toggle(id, label string) {
	values := dialog.chosen[id]
	for index, value := range values {
		if value == label {
			dialog.chosen[id] = append(values[:index], values[index+1:]...)
			return
		}
	}
	dialog.chosen[id] = append(values, label)
}

func (model appModel) advanceUserInputDialog() (tea.Model, tea.Cmd) {
	dialog := model.userInputDialog
	dialog.question++
	dialog.selected, dialog.other, dialog.text = 0, false, ""
	if dialog.question < len(model.userInputRequest.Questions) {
		return model, nil
	}
	requestID := model.userInputRequest.RequestID
	response := protocol.RequestUserInputResponse{Answers: make(map[string]protocol.RequestUserInputAnswer, len(dialog.chosen))}
	for id, answers := range dialog.chosen {
		response.Answers[id] = protocol.RequestUserInputAnswer{Answers: append([]string(nil), answers...)}
	}
	model.userInputRequest, model.userInputDialog = nil, nil
	model.status = "working"
	return model, func() tea.Msg {
		if err := model.app.options.Application.ResolveUserInput(context.WithoutCancel(model.ctx), requestID, response); err != nil {
			return operationFailedMsg{operation: "resolve user input", err: err}
		}
		return nil
	}
}

func (model appModel) renderRequestUserInputDialog(width int) string {
	request, dialog := model.userInputRequest, model.userInputDialog
	if request == nil || dialog == nil || dialog.question >= len(request.Questions) {
		return ""
	}
	question := request.Questions[dialog.question]
	rows := []string{model.palette.separator().Render(strings.Repeat("─", maxInt(1, width))), " " + model.palette.strong().Render(question.Header), "", " " + question.Question}
	if dialog.other {
		return strings.Join(append(rows, "", "   Other: "+dialog.text+"█", "", " Enter submit · Esc back"), "\n")
	}
	for index, option := range question.Options {
		marker := "  "
		if index == dialog.selected {
			marker = "› "
		}
		checked := ""
		for _, value := range dialog.chosen[question.ID] {
			if value == option.Label {
				checked = "[x] "
			}
		}
		rows = append(rows, " "+marker+checked+option.Label+" — "+model.palette.dim().Render(option.Description))
	}
	marker := "  "
	if dialog.selected == len(question.Options) {
		marker = "› "
	}
	rows = append(rows, " "+marker+"Other — Type a free-form answer", "", " ↑/↓ choose · Space toggle · Enter continue · Esc cancel")
	return strings.Join(rows, "\n")
}
