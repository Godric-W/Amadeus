package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
	tea "github.com/charmbracelet/bubbletea"
)

func (model fullscreenModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width = maxInt(40, message.Width)
		model.height = maxInt(10, message.Height)
		model.updateInputLayout()
		model.renderer, _ = newFullscreenMarkdownRenderer(maxInt(20, model.width-6), model.palette)
		model.resizeTranscriptViewport()
		return model, nil
	case fullscreenAppEventMsg:
		command := model.handleAppEvent(message.event)
		if model.viewingDetails && model.details != nil && !model.details.Empty() {
			model.refreshTranscriptViewport()
		}
		return model, tea.Batch(command, model.flushHistory())
	case fullscreenOperationFailedMsg:
		model.handleOperationFailure(message)
		return model, model.flushHistory()
	case fullscreenWorkingTickMsg:
		if !model.running || model.approval != nil {
			return model, nil
		}
		return model, model.workingTick()
	case tea.MouseMsg:
		model.lastMouseEvent = time.Now()
		return model, nil
	case tea.KeyMsg:
		if model.isRecentMouseControlFragment(message) {
			model.lastMouseEvent = time.Now()
			return model, nil
		}
		if isTerminalControlResponse(message) {
			return model, nil
		}
		if model.viewingDetails {
			return model.handleDetailViewerKey(message)
		}
		if model.approvalDialog != nil {
			return model.handleApprovalKey(message)
		}
		if model.selection != nil {
			return model.handleSelectionKey(message)
		}
		return model.handleInputKey(message)
	}
	return model, nil
}

func (model *fullscreenModel) handleOperationFailure(message fullscreenOperationFailedMsg) {
	if message.err == nil {
		return
	}
	if errors.Is(message.err, context.Canceled) {
		model.insertHistoryCell(NewNoticeHistoryCell(message.operation + " cancelled"))
	} else {
		model.insertHistoryCell(NewErrorHistoryCell(message.operation + ": " + message.err.Error()))
	}
	switch message.operation {
	case "submit task", "compact context":
		model.running = false
		model.status = "idle"
	case "set collaboration mode":
		model.pendingModeTask = ""
		model.status = "idle"
	}
}

func (model fullscreenModel) handleInputKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	model.slashPopup.sync(model.input.Value(), model.running)
	switch key.String() {
	case "shift+tab":
		if model.running {
			model.insertHistoryCell(NewNoticeHistoryCell("Collaboration mode cannot change while a task is in progress."))
			return model, model.flushHistory()
		}
		nextMode := turn.ModeKindPlan
		if model.collaboration == CollaborationPlan {
			nextMode = turn.ModeKindDefault
		}
		model.pendingModeTask = ""
		model.status = "switching mode"
		return model, model.setMode(nextMode)
	case "ctrl+c":
		if model.running {
			model.status = "cancelling"
			return model, model.interrupt()
		}
		model.input.Reset()
		model.historyPos = -1
		model.updateInputLayout()
		return model, nil
	case "ctrl+d":
		if !model.running && strings.TrimSpace(model.input.Value()) == "" {
			if model.shutdownRequested {
				return model, nil
			}
			model.shutdownRequested = true
			model.status = "shutting down"
			return model, model.shutdown()
		}
	case "ctrl+t":
		if model.details == nil || model.details.Empty() {
			return model, nil
		}
		model.viewingDetails = true
		model.input.Blur()
		model.refreshTranscriptViewport()
		return model, nil
	case "esc":
		if model.slashPopup.active() {
			model.slashPopup.dismiss(model.input.Value())
			return model, nil
		}
		if model.running && strings.TrimSpace(model.input.Value()) == "" {
			model.status = "cancelling"
			return model, model.interrupt()
		}
		model.input.Reset()
		model.historyPos = -1
		model.updateInputLayout()
		return model, nil
	case "pgup", "pgdown":
		return model, nil
	case "up":
		if model.slashPopup.active() {
			model.slashPopup.move(-1)
			return model, nil
		}
		if !model.running && model.recallHistory(-1) {
			return model, nil
		}
	case "down":
		if model.slashPopup.active() {
			model.slashPopup.move(1)
			return model, nil
		}
		if !model.running && model.recallHistory(1) {
			return model, nil
		}
	case "tab":
		if selected, ok := model.slashPopup.selectedItem(); ok {
			value := "/" + selected.Name()
			if selected.SupportsInlineArgs() {
				value += " "
			}
			model.input.SetValue(value)
			model.input.CursorEnd()
			model.slashPopup.dismiss(value)
			model.updateInputLayout()
			return model, nil
		}
	case "enter":
		if selected, ok := model.slashPopup.selectedItem(); ok {
			invocation := SlashInvocation{Command: selected}
			command := invocation.String()
			model.input.Reset()
			model.slashPopup.dismiss("")
			model.updateInputLayout()
			model.history = append(model.history, command)
			model.historyPos = -1
			return model.dispatchCommand(invocation)
		}
		text := strings.TrimSpace(model.input.Value())
		if text == "" {
			return model, nil
		}
		input, err := ParseInput(text)
		if err != nil {
			model.input.Reset()
			model.slashPopup.dismiss("")
			model.updateInputLayout()
			model.insertHistoryCell(NewErrorHistoryCell(err.Error()))
			return model, model.flushHistory()
		}
		if input.Command != nil {
			model.input.Reset()
			model.slashPopup.dismiss("")
			model.updateInputLayout()
			if model.running && !input.Command.Command.AvailableDuringTask() {
				model.insertHistoryCell(NewNoticeHistoryCell("This command is disabled while a task is in progress."))
				return model, model.flushHistory()
			}
			model.history = append(model.history, text)
			model.historyPos = -1
			return model.dispatchCommand(*input.Command)
		}
		text = input.Text
		model.input.Reset()
		model.updateInputLayout()
		model.history = append(model.history, text)
		model.historyPos = -1
		model.insertHistoryCell(NewUserMessageCell(text))
		model.details = newTranscriptDetailStore(0, 0)
		model.running = true
		model.runStartedAt = time.Now()
		model.motionStartedAt = model.runStartedAt
		model.transcript.HadWorkActivity = false
		model.transcript.NeedsFinalMessageSeparator = false
		model.status = taskPhase(TaskSubmission{Content: text, Mode: model.collaboration})
		model.draft = ""
		return model, tea.Batch(model.flushHistory(), model.submitTask(TaskSubmission{Content: text, Mode: model.collaboration}), model.workingTick())
	}
	var command tea.Cmd
	model.input, command = model.input.Update(key)
	model.sanitizeInput()
	model.slashPopup.resetDismissal(model.input.Value())
	model.slashPopup.sync(model.input.Value(), model.running)
	model.updateInputLayout()
	return model, command
}

func (model fullscreenModel) interrupt() tea.Cmd {
	return func() tea.Msg {
		if err := model.app.options.Application.Interrupt(model.ctx); err != nil {
			return fullscreenOperationFailedMsg{operation: "interrupt task", err: err}
		}
		return nil
	}
}
