package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	application "github.com/Godric-W/Amadeus/internal/app"
	tea "github.com/charmbracelet/bubbletea"
)

func (model fullscreenModel) dispatchCommand(invocation SlashInvocation) (tea.Model, tea.Cmd) {
	slashCommand, arguments := invocation.Command, invocation.Args
	if command, ok := FindSlashCommand(slashCommand.Name()); !ok || command != slashCommand {
		model.insertHistoryCell(NewErrorHistoryCell(fmt.Sprintf("unknown command %q", invocation.String())))
		return model, model.flushHistory()
	}
	if model.running && !slashCommand.AvailableDuringTask() {
		model.insertHistoryCell(NewErrorHistoryCell(fmt.Sprintf("'/%s' is disabled while a task is in progress.", slashCommand)))
		return model, model.flushHistory()
	}
	if err := ValidateSlashCommandArguments(slashCommand, arguments); err != nil {
		model.insertHistoryCell(NewErrorHistoryCell(err.Error()))
		return model, model.flushHistory()
	}
	switch slashCommand {
	case SlashStatus:
		model.insertHistoryCell(NewStatusHistoryCell(model.app.options.Application.Status()))
		return model, model.flushHistory()
	case SlashPlan:
		task := strings.TrimSpace(arguments)
		if task == "" {
			if model.pendingMode.Valid() || model.session.mode() == turn.ModeKindPlan {
				return model, nil
			}
			model.pendingMode = turn.ModeKindPlan
			model.status = "switching to Plan mode"
			return model, model.setMode(turn.ModeKindPlan)
		}
		submission := model.prepareUserMessageSubmission(UserMessage{Text: task}, turn.ModeKindPlan, true)
		return model, tea.Batch(model.flushHistory(), model.submitUserMessage(submission))
	case SlashExit:
		return model, model.requestExit(ExitModeShutdownFirst, ExitReasonUserRequested, nil)
	case SlashCopy:
		if strings.TrimSpace(model.transcript.LastAgentMarkdown) == "" {
			model.insertHistoryCell(NewErrorHistoryCell("No agent response to copy"))
			return model, model.flushHistory()
		}
		if err := model.app.options.ClipboardWrite(model.transcript.LastAgentMarkdown); err != nil {
			model.insertHistoryCell(NewErrorHistoryCell("Copy failed: " + err.Error()))
		} else {
			model.insertHistoryCell(NewNoticeHistoryCell("Copied last message to clipboard"))
		}
		return model, model.flushHistory()
	case SlashResume:
		if arguments != "" {
			threadID, err := protocol.ParseThreadID(arguments)
			if err != nil {
				model.insertHistoryCell(NewErrorHistoryCell("Invalid session ID: " + err.Error()))
				return model, model.flushHistory()
			}
			model.status = "resuming session"
			return model, func() tea.Msg {
				model.app.options.Application.Resume(model.ctx, threadID)
				return nil
			}
		}
		model.status = "loading sessions"
		return model, model.loadSessions()
	case SlashRename:
		if arguments != "" {
			model.status = "renaming session"
			return model, model.rename(arguments)
		}
		model.selection = &selectionOverlay{Title: "Rename session", Subtitle: "Type a name and press Enter", Input: true, Value: model.session.Title, Hint: "Esc cancel"}
		model.selectionKind = "rename"
		model.input.Blur()
		return model, nil
	case SlashDelete:
		model.selection = &selectionOverlay{Title: "Delete this session?", Subtitle: "Cannot be undone.", Items: []selectionItem{
			{Name: "No, keep this session", Description: "Return to the current session"},
			{Name: "Yes, delete and exit", Description: "Permanently delete this session now"},
		}}
		model.selectionKind = "delete"
		return model, nil
	case SlashCompact:
		model.running = true
		model.status = "compacting context"
		model.runStartedAt = time.Now()
		model.motionStartedAt = model.runStartedAt
		return model, tea.Batch(model.submitCompact(), model.workingTick())
	case SlashSkills:
		model.selection = &selectionOverlay{Title: "Skills", Subtitle: "Choose an action", Items: []selectionItem{
			{Name: "List skills", Description: "Browse available skills"},
			{Name: "Enable/Disable Skills", Description: "Enable or disable skills", Disabled: model.running, DisabledReason: "unavailable while a task is running"},
		}}
		model.selectionKind = "skills-menu"
		return model, nil
	case SlashMCP:
		model.mcpRequestID++
		requestID := model.mcpRequestID
		detail := application.MCPDetailSummary
		if strings.EqualFold(arguments, "verbose") {
			detail = application.MCPDetailVerbose
		}
		model.status = "loading MCP inventory"
		model.insertHistoryCell(NewMCPCommandHistoryCell())
		return model, tea.Sequence(model.flushHistory(), func() tea.Msg {
			model.app.options.Application.LoadMCP(model.ctx, requestID, detail)
			return nil
		})
	case SlashClear:
		model.clearing = true
		model.status = "starting new chat"
		return model, func() tea.Msg {
			model.app.options.Application.Clear(model.ctx)
			return nil
		}
	default:
		model.insertHistoryCell(NewErrorHistoryCell(fmt.Sprintf("Command /%s is unavailable", slashCommand)))
		return model, model.flushHistory()
	}
}

func (model fullscreenModel) loadSessions() tea.Cmd {
	return func() tea.Msg {
		model.app.options.Application.LoadSessions(model.ctx)
		return nil
	}
}

func (model fullscreenModel) submitUserMessage(submission UserMessageSubmission) tea.Cmd {
	return func() tea.Msg {
		overrides := protocol.ThreadSettingsOverrides{}
		if submission.OverrideMode && submission.Mode.Valid() {
			overrides.CollaborationMode = &protocol.CollaborationMode{Mode: protocol.ModeKind(submission.Mode)}
		}
		admission, err := model.app.options.Application.SubmitUser(model.ctx, submission.Message.Text, submission.ClientUserMessageID, overrides)
		if err != nil {
			return fullscreenUserMessageRejectedMsg{submission: submission, err: err}
		}
		return fullscreenUserMessageAdmittedMsg{submission: submission, admission: admission}
	}
}

func (model fullscreenModel) submitCompact() tea.Cmd {
	return func() tea.Msg {
		if err := model.app.options.Application.SubmitCompact(model.ctx); err != nil {
			return fullscreenOperationFailedMsg{operation: "compact context", err: err}
		}
		return nil
	}
}

func (model fullscreenModel) setMode(mode turn.ModeKind) tea.Cmd {
	return func() tea.Msg {
		if err := model.app.options.Application.SetMode(model.ctx, mode); err != nil {
			return fullscreenOperationFailedMsg{operation: "set collaboration mode", err: err}
		}
		return nil
	}
}

func (model fullscreenModel) rename(name string) tea.Cmd {
	return func() tea.Msg {
		model.app.options.Application.Rename(model.ctx, model.session.Generation, name)
		return nil
	}
}
