package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
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
		if model.exit.active() {
			model.captureExitAppEvent(message.event)
			return model, nil
		}
		command := model.handleAppEvent(message.event)
		if model.viewingDetails && model.details != nil && !model.details.Empty() {
			model.refreshTranscriptViewport()
		}
		return model, tea.Batch(command, model.flushHistory())
	case fullscreenOperationFailedMsg:
		model.handleOperationFailure(message)
		return model, model.flushHistory()
	case fullscreenUserMessageAdmittedMsg:
		model.handleUserMessageAdmission(message)
		return model, nil
	case fullscreenUserMessageRejectedMsg:
		model.handleUserMessageRejection(message)
		return model, model.flushHistory()
	case fullscreenStartupReadyMsg:
		return model, model.submitInitialUserMessageIfPending()
	case statusLineBranchUpdatedMsg:
		model.applyStatusLineBranchUpdate(message)
		return model, nil
	case fullscreenWorkingTickMsg:
		if (!model.running && !model.retryStatus.active) || model.approval != nil || model.userInputDialog != nil {
			return model, nil
		}
		return model, model.workingTick()
	case fullscreenShutdownFinishedMsg:
		return model, model.completeShutdown(message.err)
	case fullscreenShutdownTimeoutMsg:
		return model, model.expireShutdown()
	case fullscreenExitFrameDrainedMsg:
		return model, model.finishExitFrameDrain()
	case tea.MouseMsg:
		model.lastMouseEvent = time.Now()
		return model, nil
	case tea.KeyMsg:
		if model.exit.active() {
			return model, nil
		}
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
		if model.userInputDialog != nil {
			return model.handleRequestUserInputKey(message)
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
		model.pendingMode = ""
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
		if model.pendingMode.Valid() {
			return model, nil
		}
		nextMode := turn.ModeKindPlan
		if model.session.mode() == turn.ModeKindPlan {
			nextMode = turn.ModeKindDefault
		}
		model.pendingMode = nextMode
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
		if strings.TrimSpace(model.input.Value()) == "" {
			return model, model.requestExit(ExitModeShutdownFirst, ExitReasonUserRequested, nil)
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
		if model.running || model.nextTurnQueue.StartPending() {
			return model.queueComposerInput()
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
		if model.nextTurnQueue.StartPending() {
			input.Queue = true
			return model.enqueueInputResult(input)
		}
		model.input.Reset()
		model.updateInputLayout()
		message := UserMessage{Text: text}
		model.recordUserMessageHistory(message)
		submission := model.prepareUserMessageSubmission(message, model.session.mode(), false)
		return model, tea.Batch(model.flushHistory(), model.submitUserMessage(submission))
	}
	var command tea.Cmd
	model.input, command = model.input.Update(key)
	model.sanitizeInput()
	model.slashPopup.resetDismissal(model.input.Value())
	model.slashPopup.sync(model.input.Value(), model.running)
	model.updateInputLayout()
	return model, command
}

func (model *fullscreenModel) prepareUserMessageSubmission(message UserMessage, mode turn.ModeKind, overrideMode bool) UserMessageSubmission {
	model.nextClientUserMessage++
	clientID := fmt.Sprintf("tui-user-%d-%d", model.session.Generation, model.nextClientUserMessage)
	if model.optimisticUserMessages == nil {
		model.optimisticUserMessages = make(map[string]string)
	}
	model.optimisticUserMessages[clientID] = message.Text
	model.flushCompletedActivityBeforeBoundary()
	model.insertHistoryCell(NewUserMessageCell(message.Text))
	return UserMessageSubmission{Message: message, ClientUserMessageID: clientID, Mode: mode, OverrideMode: overrideMode}
}

func (model *fullscreenModel) handleUserMessageAdmission(message fullscreenUserMessageAdmittedMsg) {
	if message.submission.FromNextTurnQueue && (message.submission.OriginThreadID != model.session.ThreadID || message.submission.OriginGeneration != model.session.Generation) {
		return
	}
	if err := message.admission.Validate(); err != nil {
		model.handleUserMessageRejection(fullscreenUserMessageRejectedMsg{submission: message.submission, err: err})
		return
	}
	if !message.submission.FromNextTurnQueue || message.admission.Kind == protocol.UserMessageAdmissionStarted {
		return
	}
	if message.admission.Kind == protocol.UserMessageAdmissionSteered {
		model.nextTurnQueue.AcceptUnexpectedSteer(message.submission)
		model.insertHistoryCell(NewDiagnosticHistoryCell("queued input was admitted into an active turn; automatic queue drain stopped"))
	}
}

func (model *fullscreenModel) handleUserMessageRejection(message fullscreenUserMessageRejectedMsg) {
	delete(model.optimisticUserMessages, message.submission.ClientUserMessageID)
	if message.submission.FromNextTurnQueue {
		if message.submission.OriginThreadID != model.session.ThreadID || message.submission.OriginGeneration != model.session.Generation {
			return
		}
		model.restoreRejectedQueuedInput(message.submission)
	} else if strings.TrimSpace(model.input.Value()) == "" {
		model.input.SetValue(message.submission.Message.Text)
		model.input.CursorEnd()
		model.updateInputLayout()
	}
	if message.err != nil && !errors.Is(message.err, context.Canceled) {
		model.insertHistoryCell(NewErrorHistoryCell("submit task: " + message.err.Error()))
	}
}

func (model fullscreenModel) interrupt() tea.Cmd {
	return func() tea.Msg {
		if err := model.app.options.Application.Interrupt(model.ctx); err != nil {
			return fullscreenOperationFailedMsg{operation: "interrupt task", err: err}
		}
		return nil
	}
}
