package tui

import (
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/llm"
	tea "github.com/charmbracelet/bubbletea"
)

type fullscreenSessionState struct {
	Generation    uint64
	SessionID     protocol.SessionID
	ThreadID      protocol.ThreadID
	Title         string
	Configuration protocol.SessionConfiguration
	Usage         llm.Usage
	ContextUsed   int64
	ContextWindow int64
}

func (state *fullscreenSessionState) applyConfiguration(configuration protocol.SessionConfiguration) bool {
	previousCWD := strings.TrimSpace(state.Configuration.CWD)
	state.Configuration = configuration.Clone()
	return previousCWD != strings.TrimSpace(state.Configuration.CWD)
}

func (model *fullscreenModel) applyThreadViewSnapshot(snapshot application.ThreadViewSnapshot) tea.Cmd {
	model.session = fullscreenSessionState{
		Generation: snapshot.Generation, SessionID: snapshot.SessionID, ThreadID: snapshot.ThreadID, Title: snapshot.Title,
		Usage: snapshot.Usage, ContextUsed: snapshot.Usage.TotalTokens, ContextWindow: snapshot.ContextWindow,
	}
	model.workspace = statusLineWorkspaceState{}
	return model.applySessionConfiguration(snapshot.Configuration)
}

func (model *fullscreenModel) applySessionConfigured(event protocol.SessionConfiguredEvent) tea.Cmd {
	model.session.SessionID = event.SessionID
	model.session.ThreadID = event.ThreadID
	return model.applySessionConfiguration(event.Configuration)
}

func (state *fullscreenSessionState) applyTokenCount(event protocol.TokenCountEvent) {
	state.Usage = event.Usage
	if event.EstimatedInputTokens > 0 {
		state.ContextUsed = event.EstimatedInputTokens
	}
	if event.Usage.InputTokens > 0 {
		state.ContextUsed = event.Usage.InputTokens
	}
	if event.ContextWindow > 0 {
		state.ContextWindow = event.ContextWindow
	}
}

func (state fullscreenSessionState) mode() turn.ModeKind {
	if state.Configuration.Mode == protocol.ModeKindPlan {
		return turn.ModeKindPlan
	}
	return turn.ModeKindDefault
}

func (model *fullscreenModel) applySessionConfiguration(configuration protocol.SessionConfiguration) tea.Cmd {
	currentDirChanged := model.session.applyConfiguration(configuration)
	model.refreshStatusLine()
	if currentDirChanged {
		return model.resetStatusLineWorkspace()
	}
	return nil
}
