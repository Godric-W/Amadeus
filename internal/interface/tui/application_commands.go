package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (model fullscreenModel) submitCommand(command string) (tea.Model, tea.Cmd) {
	spec, arguments, ok := ParseSlashCommand(command)
	if !ok {
		model.entries = append(model.entries, fullscreenEntry{kind: "error", content: fmt.Sprintf("Unknown command %q", strings.Fields(command)[0])})
		return model, model.flushTranscript()
	}
	if err := ValidateSlashCommandArguments(spec, arguments); err != nil {
		model.entries = append(model.entries, fullscreenEntry{kind: "error", content: err.Error()})
		return model, model.flushTranscript()
	}
	if model.running && !spec.AvailableDuringRun {
		model.entries = append(model.entries, fullscreenEntry{kind: "error", content: fmt.Sprintf("'/%s' is disabled while a task is in progress.", spec.Command)})
		return model, model.flushTranscript()
	}
	switch spec.Command {
	case SlashStatus:
		localStatus := model.commandStatus()
		if model.app.options.Command == nil {
			model.entries = append(model.entries, fullscreenEntry{kind: "notice", content: localStatus})
			return model, model.flushTranscript()
		}
		return model, func() tea.Msg {
			output, err := model.app.options.Command(model.ctx, command)
			if strings.TrimSpace(output) == "" {
				output = localStatus
			} else {
				output = localStatus + "\n" + strings.TrimSpace(output)
			}
			return fullscreenCommandDoneMsg{command: command, output: output, err: err}
		}
	case SlashPlan:
		model.collaboration = CollaborationPlan
		model.status = "plan mode"
		model.entries = append(model.entries, fullscreenEntry{kind: "notice", content: "Switched to Plan mode"})
		return model, model.flushTranscript()
	case SlashExit:
		return model, tea.Quit
	case SlashCopy:
		if strings.TrimSpace(model.lastAssistantMarkdown) == "" {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: "No agent response to copy"})
			return model, model.flushTranscript()
		}
		if err := model.app.options.ClipboardWrite(model.lastAssistantMarkdown); err != nil {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: "Copy failed: " + err.Error()})
		} else {
			model.entries = append(model.entries, fullscreenEntry{kind: "notice", content: "Copied last response to clipboard"})
		}
		return model, model.flushTranscript()
	case SlashResume:
		if model.app.options.Sessions == nil || model.app.options.Resume == nil {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: "Session picker is unavailable"})
			return model, model.flushTranscript()
		}
		if arguments != "" {
			model.status = "resuming session"
			return model, func() tea.Msg {
				message, err := model.app.options.Resume(model.ctx, arguments)
				return fullscreenResumeMsg{message: message, err: err}
			}
		}
		model.status = "loading sessions"
		return model, model.loadSessions()
	case SlashRename:
		if model.app.options.Rename == nil {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: "Session rename is unavailable"})
			return model, model.flushTranscript()
		}
		if arguments != "" {
			model.status = "renaming session"
			return model, func() tea.Msg {
				message, err := model.app.options.Rename(model.ctx, arguments)
				return fullscreenRenameMsg{message: message, err: err}
			}
		}
		currentTitle := ""
		if model.app.options.CurrentSessionTitle != nil {
			currentTitle = model.app.options.CurrentSessionTitle()
		}
		model.selection = &selectionOverlay{Title: "Rename session", Subtitle: "Type a name and press Enter", Input: true, Value: currentTitle, Hint: "Esc cancel"}
		model.selectionKind = "rename"
		model.input.Blur()
		return model, nil
	case SlashDelete:
		if model.app.options.Delete == nil {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: "Session deletion is unavailable"})
			return model, model.flushTranscript()
		}
		model.selection = &selectionOverlay{Title: "Delete this session?", Subtitle: "Cannot be undone.", Items: []selectionItem{
			{Name: "No, keep this session", Description: "Return to the current session"},
			{Name: "Yes, delete and exit", Description: "Permanently delete this session now"},
		}}
		model.selectionKind = "delete"
		return model, nil
	case SlashCompact:
		if model.app.options.Compact == nil {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: "Conversation compaction is unavailable"})
			return model, model.flushTranscript()
		}
		model.status = "compacting"
		return model, func() tea.Msg {
			message, err := model.app.options.Compact(model.ctx)
			return fullscreenCompactMsg{message: message, err: err}
		}
	case SlashSkills:
		if model.app.options.Skills == nil {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: "Skills are unavailable"})
			return model, model.flushTranscript()
		}
		model.selection = &selectionOverlay{Title: "Skills", Subtitle: "Choose an action", Items: []selectionItem{
			{Name: "List skills", Description: "Browse available skills"},
			{Name: "Enable/Disable Skills", Description: "Enable or disable skills", Disabled: model.running, DisabledReason: "unavailable while a task is running"},
		}}
		model.selectionKind = "skills-menu"
		return model, nil
	default:
		if model.app.options.Command == nil {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: fmt.Sprintf("Command /%s is unavailable", spec.Command)})
			return model, model.flushTranscript()
		}
		model.status = "running command"
		return model, func() tea.Msg {
			output, err := model.app.options.Command(model.ctx, command)
			return fullscreenCommandDoneMsg{command: command, output: output, err: err}
		}
	}
}

func (model fullscreenModel) commandStatus() string {
	mode := string(model.collaboration)
	if mode == "" {
		mode = string(CollaborationExecute)
	}
	return fmt.Sprintf("mode: %s\nmodel: %s\ntokens: input=%d cached/unknown output=%d context=%d/%d\nphase: %s",
		mode, strings.TrimSpace(model.model), model.inputUsage, model.outputUsage, model.contextUsage, model.contextLimit, model.status)
}

func taskPhase(task TaskSubmission) string {
	if task.Mode == CollaborationPlan {
		return "planning"
	}
	return "executing"
}

func (model fullscreenModel) loadSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, err := model.app.options.Sessions(model.ctx)
		return fullscreenSessionsMsg{sessions: sessions, err: err}
	}
}

func (model fullscreenModel) runTask(task TaskSubmission) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel, err := model.app.options.NewTask(model.ctx)
		if err != nil {
			return fullscreenTaskDoneMsg{err: err}
		}
		model.app.setActiveRun(cancel)
		defer func() {
			cancel()
			model.app.clearActiveRun(cancel)
		}()
		taskErr := model.app.options.Task(ctx, task)
		session := ""
		if model.app.options.CurrentSession != nil {
			session = model.app.options.CurrentSession()
		}
		return fullscreenTaskDoneMsg{err: taskErr, session: session}
	}
}

func (model *fullscreenModel) refreshCurrentSession() {
	if model != nil && model.app.options.CurrentSession != nil {
		if session := strings.TrimSpace(model.app.options.CurrentSession()); session != "" {
			model.startup.Session = session
		}
	}
}
