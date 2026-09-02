package tui

import (
	"strings"
	"time"

	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func (model appModel) uiNow() time.Time {
	if model.clock != nil {
		return model.clock.Now()
	}
	return time.Now()
}

type sessionViewState struct {
	Generation       uint64
	SessionID        protocol.SessionID
	ThreadID         protocol.ThreadID
	Title            string
	Configuration    protocol.SessionConfiguration
	TokenInfo        *protocol.TokenUsageInfo
	Goal             *protocol.ThreadGoal
	GoalsEnabled     bool
	ContextUsed      int64
	ContextWindow    int64
	ContextEstimated bool
}

func (state *sessionViewState) applyConfiguration(configuration protocol.SessionConfiguration) bool {
	previousCWD := strings.TrimSpace(state.Configuration.CWD)
	state.Configuration = configuration.Clone()
	return previousCWD != strings.TrimSpace(state.Configuration.CWD)
}

func (model *appModel) applyThreadViewSnapshot(snapshot application.ThreadViewSnapshot) tea.Cmd {
	model.session = sessionViewState{
		Generation: snapshot.Generation, SessionID: snapshot.SessionID, ThreadID: snapshot.ThreadID, Title: snapshot.Title,
		TokenInfo: cloneTUITokenInfo(snapshot.TokenInfo), Goal: cloneTUIGoal(snapshot.Goal), GoalsEnabled: snapshot.GoalsEnabled, ContextUsed: snapshot.ActiveContextTokens,
		ContextWindow: tokenInfoContextWindow(snapshot.TokenInfo), ContextEstimated: snapshot.ActiveContextEstimated,
	}
	model.workspace = statusLineWorkspaceState{}
	model.goalObservedAt = model.uiNow()
	model.goalActiveTurnStartedAt = time.Time{}
	return model.applySessionConfiguration(snapshot.Configuration)
}

func (state *sessionViewState) applyGoal(goal *protocol.ThreadGoal) {
	state.Goal = cloneTUIGoal(goal)
}

func (model *appModel) setGoalSnapshot(goal *protocol.ThreadGoal, observedAt time.Time) {
	model.session.applyGoal(goal)
	model.goalObservedAt = observedAt
	if goal == nil || goal.Status != protocol.ThreadGoalActive {
		model.goalActiveTurnStartedAt = time.Time{}
	}
	model.refreshGoalIndicatorAt(observedAt)
}

func cloneTUIGoal(goal *protocol.ThreadGoal) *protocol.ThreadGoal {
	if goal == nil {
		return nil
	}
	cloned := *goal
	if goal.TokenBudget != nil {
		budget := *goal.TokenBudget
		cloned.TokenBudget = &budget
	}
	return &cloned
}

func (model *appModel) applySessionConfigured(event protocol.SessionConfiguredEvent) tea.Cmd {
	model.session.SessionID = event.SessionID
	model.session.ThreadID = event.ThreadID
	return model.applySessionConfiguration(event.Configuration)
}

func (state *sessionViewState) applyTokenCount(event protocol.TokenCountEvent) {
	state.TokenInfo = cloneTUITokenInfo(event.Info)
	state.ContextUsed = event.ActiveContextTokens
	state.ContextEstimated = event.ActiveContextEstimated
	state.ContextWindow = tokenInfoContextWindow(event.Info)
}

func (state sessionViewState) totalTokenUsage() (usage llm.TokenUsage) {
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

func (state sessionViewState) mode() protocol.ModeKind {
	if state.Configuration.Mode == protocol.ModeKindPlan {
		return protocol.ModeKindPlan
	}
	return protocol.ModeKindDefault
}

func (model *appModel) applySessionConfiguration(configuration protocol.SessionConfiguration) tea.Cmd {
	currentDirChanged := model.session.applyConfiguration(configuration)
	model.TranscriptSurface.setSessionHeader(NewSessionHeaderCell(
		model.startup.Version,
		model.session.Configuration.Model,
		model.session.Configuration.CWD,
	))
	model.refreshStatusLine()
	if currentDirChanged {
		return model.resetStatusLineWorkspace()
	}
	return nil
}
