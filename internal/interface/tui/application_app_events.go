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

func (model *fullscreenModel) handleAppEvent(event application.InteractiveEvent) tea.Cmd {
	switch event := event.(type) {
	case application.SessionEventObserved:
		if event.Generation != model.generation || protocol.ThreadIDOf(event.Event.Msg) != protocol.ThreadID(applicationThreadID(model.startup.Session)) {
			return nil
		}
		return model.applyEvent(event.Event)
	case application.ApprovalRequested:
		if event.Generation != model.generation {
			return nil
		}
		model.approval = &fullscreenApproval{requestID: event.RequestID, request: event.Request}
		model.approvalDialog = newApprovalDialog(event.Request)
		if cell, ok := model.transcript.ActiveCell.(*ToolHistoryCell); ok && cell.SetApprovalState(event.Request.ID, true) {
			model.transcript.bumpActiveCellRevision()
		}
		model.status = "awaiting approval"
		model.input.Blur()
	case application.ThreadAttached:
		return model.attachSnapshot(event.Snapshot)
	case application.ThreadAttachFailed:
		model.clearing = false
		model.selection = nil
		model.selectionKind = ""
		model.sessions = nil
		model.status = "idle"
		model.insertHistoryCell(NewErrorHistoryCell("switch session: " + errorText(event.Error)))
		return model.input.Focus()
	case application.SessionsLoaded:
		model.status = "idle"
		if event.Error != nil {
			model.insertHistoryCell(NewErrorHistoryCell(event.Error.Error()))
			return nil
		}
		model.openSessions(event.Sessions)
	case application.ThreadNameUpdated:
		if event.Generation != model.generation || event.ThreadID != applicationThreadID(model.startup.Session) {
			return nil
		}
		model.selection = nil
		model.selectionKind = ""
		model.startup.Session = string(event.ThreadID)
		model.sessionTitle = event.Name
		model.status = "idle"
		model.insertHistoryCell(NewNoticeHistoryCell("Session renamed to " + event.Name))
	case application.ThreadRenameFailed:
		model.status = "idle"
		model.insertHistoryCell(NewErrorHistoryCell("rename session: " + errorText(event.Error)))
	case application.ThreadDeleted:
		model.selection = nil
		model.selectionKind = ""
		model.status = "deleted"
		return tea.Quit
	case application.ThreadDeleteFailed:
		model.selection = nil
		model.selectionKind = ""
		model.status = "idle"
		model.insertHistoryCell(NewErrorHistoryCell("delete session: " + errorText(event.Error)))
		return model.input.Focus()
	case application.ClearUIStarted:
		model.clearInteractiveState()
		model.clearing = true
		model.status = "starting new chat"
		return tea.Sequence(func() tea.Msg { return tea.ClearScreen() }, tea.Println(model.banner()))
	case application.MCPInventoryLoaded:
		if event.RequestID != model.mcpRequestID || event.Generation != model.generation || event.ThreadID != applicationThreadID(model.startup.Session) {
			return nil
		}
		var cell HistoryCell
		if event.Error != nil {
			cell = NewErrorHistoryCell("load MCP inventory: " + event.Error.Error())
		} else if len(event.Inventory.Servers) == 0 {
			cell = NewEmptyMCPInventoryCell()
		} else {
			cell = NewMCPInventoryCell(event.Detail, event.Inventory)
		}
		model.insertHistoryCell(cell)
		model.status = "idle"
	case application.SkillsLoaded:
		if event.Generation != model.generation {
			return nil
		}
		model.status = "idle"
		if event.Error != nil {
			model.insertHistoryCell(NewErrorHistoryCell("load skills: " + event.Error.Error()))
			return nil
		}
		model.openSkills(event.Skills)
	case application.SkillEnabledSet:
		if event.Generation != model.generation {
			return nil
		}
		if event.Error != nil {
			model.insertHistoryCell(NewErrorHistoryCell("update skill: " + event.Error.Error()))
			return nil
		}
		for index := range model.skills {
			if model.skills[index].Path == event.Path {
				model.skills[index].Enabled = event.Enabled
			}
		}
		model.pendingSkillsView = "manage"
		model.status = "refreshing skills"
		return func() tea.Msg {
			model.app.options.Application.LoadSkills()
			return nil
		}
	case application.ShutdownStarted:
		model.shutdownRequested = true
		model.status = "shutting down"
		model.insertHistoryCell(NewNoticeHistoryCell("Shutting down…"))
	case application.ShutdownFinished:
		if event.Error != nil {
			model.insertHistoryCell(NewErrorHistoryCell("shutdown: " + event.Error.Error()))
		}
		return tea.Quit
	case application.ApplicationError:
		model.insertHistoryCell(NewErrorHistoryCell(event.Operation + ": " + errorText(event.Error)))
	}
	return nil
}

func (model *fullscreenModel) attachSnapshot(snapshot application.ThreadViewSnapshot) tea.Cmd {
	model.clearInteractiveState()
	model.generation = snapshot.Generation
	model.startup.Session = string(snapshot.ThreadID)
	model.startup.Provider = snapshot.Provider
	model.startup.Model = snapshot.Model
	model.startup.ContextWindow = snapshot.ContextWindow
	model.model = snapshot.Model
	model.sessionTitle = snapshot.Title
	model.collaboration = CollaborationExecute
	if snapshot.Mode == turn.ModeKindPlan {
		model.collaboration = CollaborationPlan
	}
	model.inputUsage = snapshot.Usage.InputTokens
	model.outputUsage = snapshot.Usage.OutputTokens
	model.contextUsage = snapshot.Usage.TotalTokens
	model.contextLimit = snapshot.ContextWindow
	model.runtimeTranscript = protocol.NewTranscriptState(protocol.ThreadID(snapshot.ThreadID))
	model.restoreCompletedItems(snapshot.Items)
	model.pendingHistoryCells = append([]HistoryCell(nil), model.historyCells...)
	model.hasEmittedHistoryLines = false
	model.clearing = false
	model.status = "idle"
	focus := model.input.Focus()
	return tea.Sequence(func() tea.Msg { return tea.ClearScreen() }, tea.Println(model.banner()), model.flushHistory(), focus)
}

func (model *fullscreenModel) clearInteractiveState() {
	model.clearRetryStatus()
	model.resetHistory()
	model.details = newTranscriptDetailStore(0, 0)
	model.draft = ""
	model.running = false
	model.runStartedAt = time.Time{}
	model.approval = nil
	model.approvalDialog = nil
	model.selection = nil
	model.selectionKind = ""
	model.sessions = nil
	model.skills = nil
	model.pendingSkillsView = ""
	model.pendingModeTask = ""
	model.viewingDetails = false
}

func (model *fullscreenModel) openSessions(sessions []application.SessionOption) {
	if len(sessions) == 0 {
		model.insertHistoryCell(NewNoticeHistoryCell("No saved sessions are available for this project."))
		return
	}
	model.sessions = append([]application.SessionOption(nil), sessions...)
	selected := 0
	items := make([]selectionItem, 0, len(sessions))
	for index, session := range sessions {
		description := session.Title
		if session.Current {
			description += " · current"
			selected = index
		}
		items = append(items, selectionItem{Name: string(session.ID), Description: description})
	}
	model.selection = &selectionOverlay{Title: "Resume Session", Subtitle: "Select a saved chat", Items: items, Selected: selected, Search: true, Hint: "Type to search · Esc cancel"}
	model.selectionKind = "resume"
	model.input.Blur()
}

func (model *fullscreenModel) openSkills(skills []application.SkillOption) {
	model.skills = append([]application.SkillOption(nil), skills...)
	if len(skills) == 0 {
		model.pendingSkillsView = ""
		model.insertHistoryCell(NewNoticeHistoryCell("No skills are available."))
		return
	}
	items := make([]selectionItem, 0, len(skills))
	for _, skill := range skills {
		state := "enabled"
		if !skill.Enabled {
			state = "disabled"
		}
		items = append(items, selectionItem{Name: skill.Name, Description: fmt.Sprintf("%s · %s · %s", skill.Description, skill.Source, state)})
	}
	kind := model.pendingSkillsView
	if kind != "manage" {
		kind = "list"
	}
	title := "Skills"
	hint := "Type to search · Enter insert · Esc cancel"
	if kind == "manage" {
		title = "Manage Skills"
		hint = "Type to search · Enter toggle · Esc cancel"
	}
	model.selection = &selectionOverlay{Title: title, Subtitle: "Search by name or description", Items: items, Search: true, Hint: hint}
	model.selectionKind = "skills-" + kind
	model.pendingSkillsView = ""
	model.input.Blur()
}

func errorText(err error) string {
	if err == nil || strings.TrimSpace(err.Error()) == "" {
		return "unknown error"
	}
	return err.Error()
}
