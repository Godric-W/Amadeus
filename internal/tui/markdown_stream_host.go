package tui

import (
	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type markdownStreamHost struct {
	assistant           *StreamController
	plan                *PlanStreamController
	transcriptProtected bool
	deferred            []deferredTranscriptProjection
}

type deferredTranscriptProjection struct {
	protocolEvent    protocol.EventMsg
	applicationEvent application.InteractiveEvent
	historyCell      HistoryCell
}

func (host *markdownStreamHost) active() bool {
	return host != nil && (host.assistant != nil || host.plan != nil)
}

func (host *markdownStreamHost) protectingTranscript() bool {
	return host != nil && host.transcriptProtected
}

func (host *markdownStreamHost) deferProtocol(message protocol.EventMsg) {
	if host != nil && message != nil {
		host.deferred = append(host.deferred, deferredTranscriptProjection{protocolEvent: message})
	}
}

func (host *markdownStreamHost) deferApplication(event application.InteractiveEvent) {
	if host != nil && event != nil {
		host.deferred = append(host.deferred, deferredTranscriptProjection{applicationEvent: event})
	}
}

func (host *markdownStreamHost) deferHistoryCell(cell HistoryCell) {
	if host != nil && cell != nil {
		host.deferred = append(host.deferred, deferredTranscriptProjection{historyCell: cell})
	}
}

func (model *appModel) flushDeferredTranscriptProjections() tea.Cmd {
	if model == nil || model.markdownStreams.protectingTranscript() || len(model.markdownStreams.deferred) == 0 {
		return nil
	}
	deferred := append([]deferredTranscriptProjection(nil), model.markdownStreams.deferred...)
	model.markdownStreams.deferred = nil
	commands := make([]tea.Cmd, 0, len(deferred))
	for _, projection := range deferred {
		var command tea.Cmd
		switch {
		case projection.protocolEvent != nil:
			command = model.projectProtocolEvent(projection.protocolEvent)
		case projection.applicationEvent != nil:
			command = model.handleAppEventNow(projection.applicationEvent)
		case projection.historyCell != nil:
			model.insertHistoryCellNow(projection.historyCell)
		}
		if command != nil {
			commands = append(commands, command)
		}
	}
	if len(commands) == 0 {
		return nil
	}
	return tea.Sequence(commands...)
}

func (model *appModel) shouldDeferProtocolProjection(message protocol.EventMsg) bool {
	if model == nil || !model.markdownStreams.protectingTranscript() {
		return false
	}
	switch item := message.(type) {
	case protocol.ItemStartedEvent:
		return true
	case protocol.ItemCompletedEvent:
		if item.Item.Kind == protocol.ItemAssistantMessage && model.markdownStreams.assistant != nil && model.markdownStreams.assistant.ItemID == item.Item.ID {
			return false
		}
		if item.Item.Kind == protocol.ItemPlan && model.markdownStreams.plan != nil && model.markdownStreams.plan.ItemID == item.Item.ID {
			return false
		}
		return true
	case protocol.CommandOutputDeltaEvent, protocol.PlanUpdateEvent, protocol.WarningEvent:
		return true
	default:
		return false
	}
}

func (model *appModel) shouldDeferApplicationProjection(event application.InteractiveEvent) bool {
	if model == nil || !model.markdownStreams.protectingTranscript() {
		return false
	}
	switch event.(type) {
	case application.ApprovalRequested,
		application.UserInputRequested,
		application.ThreadAttachFailed,
		application.ThreadNameUpdated,
		application.ThreadRenameFailed,
		application.ThreadDeleteFailed,
		application.MCPInventoryLoaded,
		application.SkillsLoaded,
		application.SkillEnabledSet,
		application.ApplicationError:
		return true
	default:
		return false
	}
}
