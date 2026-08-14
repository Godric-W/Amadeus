package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
	case fullscreenEventMsg:
		model.applyEvent(message.item)
		if model.viewingDetails && model.details != nil && !model.details.Empty() {
			model.refreshTranscriptViewport()
		}
		return model, model.flushHistory()
	case fullscreenApprovalMsg:
		model.approval = message.prompt
		model.approvalDialog = newApprovalDialog(message.prompt.request)
		model.status = "awaiting approval"
		model.input.Blur()
		return model, nil
	case fullscreenTaskDoneMsg:
		runDuration := message.elapsed
		if runDuration <= 0 {
			runDuration = model.runElapsed()
		}
		model.finishDraft()
		model.flushActiveHistoryCell()
		if strings.TrimSpace(message.session) != "" {
			model.startup.Session = strings.TrimSpace(message.session)
		}
		if model.transcript.HadWorkActivity && model.transcript.NeedsFinalMessageSeparator {
			model.insertHistoryCell(FinalMessageSeparator{Elapsed: runDuration})
			model.transcript.NeedsFinalMessageSeparator = false
		}
		if message.err != nil {
			if errors.Is(message.err, context.Canceled) {
				model.insertHistoryCell(NewNoticeHistoryCell("当前任务已取消。"))
			} else {
				model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
			}
		}
		model.transcript.HadWorkActivity = false
		model.transcript.NeedsFinalMessageSeparator = false
		if len(model.queuedTasks) > 0 {
			next := model.queuedTasks[0]
			model.queuedTasks = model.queuedTasks[1:]
			model.details = newTranscriptDetailStore(0, 0)
			model.running = true
			model.runStartedAt = time.Now()
			model.motionStartedAt = model.runStartedAt
			model.status = taskPhase(next)
			model.draft = ""
			return model, tea.Sequence(model.flushHistory(), tea.Batch(model.runTask(next), model.workingTick()))
		}
		model.running = false
		model.status = "idle"
		return model, model.flushHistory()
	case fullscreenCommandDoneMsg:
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
		} else if strings.HasPrefix(strings.TrimSpace(message.command), "/clear") {
			model.resetHistory()
			model.details = newTranscriptDetailStore(0, 0)
			model.draft = ""
			model.collaboration = CollaborationExecute
			model.refreshCurrentSession()
			model.status = "idle"
			return model, tea.Sequence(func() tea.Msg { return tea.ClearScreen() }, tea.Println(model.banner()))
		} else if strings.TrimSpace(message.output) != "" {
			model.insertHistoryCell(NewNoticeHistoryCell(message.output))
		}
		model.status = "idle"
		return model, model.flushHistory()
	case fullscreenPermissionModeDoneMsg:
		if message.err != nil {
			model.status = "idle"
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
			return model, model.flushHistory()
		}
		model.collaboration = message.mode
		if strings.TrimSpace(message.task) == "" {
			if message.mode == CollaborationPlan {
				model.status = "plan mode"
				model.insertHistoryCell(NewNoticeHistoryCell("Switched to Plan mode"))
			} else {
				model.status = "idle"
				model.insertHistoryCell(NewNoticeHistoryCell("Switched to Execute mode"))
			}
			return model, model.flushHistory()
		}
		model.insertHistoryCell(NewUserMessageCell(message.task))
		model.details = newTranscriptDetailStore(0, 0)
		model.running = true
		model.runStartedAt = time.Now()
		model.motionStartedAt = model.runStartedAt
		model.transcript.HadWorkActivity = false
		model.transcript.NeedsFinalMessageSeparator = false
		submission := TaskSubmission{Content: message.task, Mode: message.mode}
		model.status = taskPhase(submission)
		model.draft = ""
		return model, tea.Sequence(model.flushHistory(), tea.Batch(model.runTask(submission), model.workingTick()))
	case fullscreenSessionsMsg:
		model.status = "idle"
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
			return model, model.flushHistory()
		}
		if len(message.sessions) == 0 {
			model.insertHistoryCell(NewNoticeHistoryCell("当前项目还没有可恢复的 Session。"))
			return model, model.flushHistory()
		}
		model.sessions = message.sessions
		selected := 0
		items := make([]selectionItem, 0, len(message.sessions))
		for index, session := range model.sessions {
			description := session.Title
			if session.Current {
				description += " · current"
			}
			items = append(items, selectionItem{Name: session.ID, Description: description})
			if session.Current {
				selected = index
			}
		}
		model.selection = &selectionOverlay{Title: "Resume Session", Subtitle: "Select a saved chat", Items: items, Selected: selected, Search: true, Hint: "Type to search · Esc cancel"}
		model.selectionKind = "resume"
		return model, nil
	case fullscreenResumeMsg:
		model.selection = nil
		model.selectionKind = ""
		model.sessions = nil
		model.status = "idle"
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
		} else if strings.TrimSpace(message.message) != "" {
			model.insertHistoryCell(NewNoticeHistoryCell(message.message))
			model.refreshCurrentSession()
			model.collaboration = CollaborationExecute
		}
		return model, model.flushHistory()
	case fullscreenRenameMsg:
		model.selection = nil
		model.selectionKind = ""
		model.status = "idle"
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
		} else {
			model.insertHistoryCell(NewNoticeHistoryCell(message.message))
			model.refreshCurrentSession()
		}
		return model, model.flushHistory()
	case fullscreenDeleteMsg:
		model.selection = nil
		model.selectionKind = ""
		model.status = "idle"
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
			return model, model.flushHistory()
		}
		model.insertHistoryCell(NewNoticeHistoryCell(message.message))
		return model, tea.Sequence(model.flushHistory(), tea.Quit)
	case fullscreenCompactMsg:
		model.status = "idle"
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
		} else {
			model.insertHistoryCell(NewNoticeHistoryCell(message.message))
		}
		return model, model.flushHistory()
	case fullscreenSkillsMsg:
		model.status = "idle"
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
			return model, model.flushHistory()
		}
		model.skills = message.skills
		if len(message.skills) == 0 {
			model.insertHistoryCell(NewNoticeHistoryCell("No skills available."))
			return model, model.flushHistory()
		}
		items := make([]selectionItem, 0, len(message.skills))
		for _, skill := range message.skills {
			state := "enabled"
			if !skill.Enabled {
				state = "disabled"
			}
			items = append(items, selectionItem{Name: skill.Name, Description: skill.Description + " · " + skill.Source + " · " + state})
		}
		model.selection = &selectionOverlay{Title: "Skills", Subtitle: "Enter toggles the selected skill", Items: items, Search: true, Hint: "Type to search · Enter toggle · Esc cancel"}
		model.selectionKind = "skills"
		return model, nil
	case fullscreenSkillSetMsg:
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
			return model, model.flushHistory()
		}
		for index := range model.skills {
			if model.skills[index].Name == message.name {
				model.skills[index].Enabled = message.enabled
				if model.selection != nil && index < len(model.selection.Items) {
					state := "enabled"
					if !message.enabled {
						state = "disabled"
					}
					model.selection.Items[index].Description = model.skills[index].Description + " · " + model.skills[index].Source + " · " + state
				}
			}
		}
		return model, nil
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

func (model fullscreenModel) handleInputKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	model.slashPopup.sync(model.input.Value(), model.running)
	switch key.String() {
	case "shift+tab":
		if model.running {
			model.insertHistoryCell(NewNoticeHistoryCell("Collaboration mode cannot change while a task is in progress."))
			return model, model.flushHistory()
		}
		if model.app.options.SetPermissionMode == nil {
			model.insertHistoryCell(NewErrorHistoryCell("Permission mode control is unavailable"))
			return model, model.flushHistory()
		}
		nextMode := CollaborationPlan
		if model.collaboration == CollaborationPlan {
			nextMode = CollaborationExecute
		}
		model.status = "switching mode"
		return model, func() tea.Msg {
			err := model.app.options.SetPermissionMode(model.ctx, nextMode)
			return fullscreenPermissionModeDoneMsg{mode: nextMode, err: err}
		}
	case "ctrl+c":
		if model.running {
			model.status = "cancelling"
			model.app.cancelActiveRun()
			return model, nil
		}
		model.input.Reset()
		model.historyPos = -1
		model.updateInputLayout()
		return model, nil
	case "ctrl+d":
		if !model.running && strings.TrimSpace(model.input.Value()) == "" {
			return model, tea.Quit
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
			model.app.cancelActiveRun()
			return model, nil
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
		if model.running {
			model.insertHistoryCell(NewUserMessageCell(text))
			model.queuedTasks = append(model.queuedTasks, TaskSubmission{Content: text, Mode: model.collaboration})
			model.status = fmt.Sprintf("%s · %d queued", model.status, len(model.queuedTasks))
			return model, model.flushHistory()
		}
		model.insertHistoryCell(NewUserMessageCell(text))
		model.details = newTranscriptDetailStore(0, 0)
		model.running = true
		model.runStartedAt = time.Now()
		model.motionStartedAt = model.runStartedAt
		model.transcript.HadWorkActivity = false
		model.transcript.NeedsFinalMessageSeparator = false
		submission := TaskSubmission{Content: text, Mode: model.collaboration}
		model.status = taskPhase(submission)
		model.draft = ""
		return model, tea.Sequence(model.flushHistory(), tea.Batch(model.runTask(submission), model.workingTick()))
	}
	var command tea.Cmd
	model.input, command = model.input.Update(key)
	model.sanitizeInput()
	model.slashPopup.resetDismissal(model.input.Value())
	model.slashPopup.sync(model.input.Value(), model.running)
	model.updateInputLayout()
	return model, command
}
