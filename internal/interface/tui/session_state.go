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
	Generation       uint64
	SessionID        protocol.SessionID
	ThreadID         protocol.ThreadID
	Title            string
	Configuration    protocol.SessionConfiguration
	TokenInfo        *protocol.TokenUsageInfo
	ContextUsed      int64
	ContextWindow    int64
	ContextEstimated bool
}

func (state *fullscreenSessionState) applyConfiguration(configuration protocol.SessionConfiguration) bool {
	previousCWD := strings.TrimSpace(state.Configuration.CWD)
	state.Configuration = configuration.Clone()
	return previousCWD != strings.TrimSpace(state.Configuration.CWD)
}

func (model *fullscreenModel) applyThreadViewSnapshot(snapshot application.ThreadViewSnapshot) tea.Cmd {
	model.session = fullscreenSessionState{
		Generation: snapshot.Generation, SessionID: snapshot.SessionID, ThreadID: snapshot.ThreadID, Title: snapshot.Title,
		TokenInfo: cloneTUITokenInfo(snapshot.TokenInfo), ContextUsed: snapshot.ActiveContextTokens,
		ContextWindow: tokenInfoContextWindow(snapshot.TokenInfo), ContextEstimated: snapshot.ActiveContextEstimated,
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
	state.TokenInfo = cloneTUITokenInfo(event.Info)
	state.ContextUsed = event.ActiveContextTokens
	state.ContextEstimated = event.ActiveContextEstimated
	state.ContextWindow = tokenInfoContextWindow(event.Info)
}

func (state fullscreenSessionState) totalTokenUsage() (usage llm.TokenUsage) {
	if state.TokenInfo != nil {
		return state.TokenInfo.TotalTokenUsage
	}
	return usage
}

func cloneTUITokenInfo(info *protocol.TokenUsageInfo) *protocol.TokenUsageInfo {
	if info == nil {
		return nil
	}
	cloned := info.Clone()
	return &cloned
}

func tokenInfoContextWindow(info *protocol.TokenUsageInfo) int64 {
	if info == nil {
		return 0
	}
	return info.ModelContextWindow
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
