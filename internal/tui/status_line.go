package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
)

type statusLineItem uint8

const (
	statusLineItemModelWithReasoning statusLineItem = iota
	statusLineItemCurrentDir
	statusLineItemGitBranch
	statusLineItemThreadTitle
	statusLineItemContextUsed
	statusLineItemContextWindowSize
)

var fixedStatusLineItems = [...]statusLineItem{
	statusLineItemModelWithReasoning,
	statusLineItemCurrentDir,
	statusLineItemGitBranch,
	statusLineItemThreadTitle,
	statusLineItemContextUsed,
	statusLineItemContextWindowSize,
}

type statusLineSegment struct {
	Item statusLineItem
	Text string
}

type statusLineState struct {
	Segments           []statusLineSegment
	ContextUsedPercent int64
}

type statusLineWorkspaceState struct {
	Generation uint64
	CurrentDir string
	Branch     string
	Pending    bool
}

func (model *appModel) refreshStatusLine() {
	segments := make([]statusLineSegment, 0, len(fixedStatusLineItems))
	for _, item := range fixedStatusLineItems {
		if value, ok := model.statusLineValueForItem(item); ok {
			segments = append(segments, statusLineSegment{Item: item, Text: value})
		}
	}
	model.footer.StatusLine = statusLineState{Segments: segments, ContextUsedPercent: model.statusLineContextUsedPercent()}
	model.footer.CollaborationIndicator = collaborationModeIndicatorFor(model.session.mode())
	model.refreshGoalIndicatorAt(model.uiNow())
}

func (model *appModel) refreshGoalIndicatorAt(now time.Time) {
	model.footer.GoalIndicator = goalIndicatorLabel(model.session.Goal, model.goalObservedAt, model.goalActiveTurnStartedAt, now)
}

func goalIndicatorLabel(goal *protocol.ThreadGoal, observedAt, activeTurnStartedAt, now time.Time) string {
	if goal == nil {
		return ""
	}
	timeUsed := maxInt64(goal.TimeUsedSeconds, 0)
	if goal.Status == protocol.ThreadGoalActive && !activeTurnStartedAt.IsZero() {
		baseline := observedAt
		if activeTurnStartedAt.After(baseline) {
			baseline = activeTurnStartedAt
		}
		if !baseline.IsZero() && now.After(baseline) {
			timeUsed += int64(now.Sub(baseline) / time.Second)
		}
	}
	switch goal.Status {
	case protocol.ThreadGoalActive:
		if goal.TokenBudget != nil {
			return fmt.Sprintf("Pursuing goal (%s / %s)", compactTokenCount(goal.TokensUsed), compactTokenCount(*goal.TokenBudget))
		}
		return "Pursuing goal (" + formatGoalElapsedSeconds(timeUsed) + ")"
	case protocol.ThreadGoalPaused:
		return "Goal paused (/goal resume)"
	case protocol.ThreadGoalBlocked:
		return "Goal stalled (/goal resume)"
	case protocol.ThreadGoalUsageLimited:
		return "Goal hit usage limits (/goal resume)"
	case protocol.ThreadGoalBudgetLimited:
		if goal.TokenBudget != nil {
			return fmt.Sprintf("Goal unmet (%s / %s tokens)", compactTokenCount(goal.TokensUsed), compactTokenCount(*goal.TokenBudget))
		}
		return "Goal abandoned"
	case protocol.ThreadGoalComplete:
		if goal.TokenBudget != nil {
			return fmt.Sprintf("Goal achieved (%s tokens)", compactTokenCount(goal.TokensUsed))
		}
		return "Goal achieved (" + formatGoalElapsedSeconds(timeUsed) + ")"
	default:
		return ""
	}
}

func formatGoalElapsedSeconds(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := minutes / 60
	if hours < 24 {
		return fmt.Sprintf("%dh %dm", hours, minutes%60)
	}
	return fmt.Sprintf("%dd %dh %dm", hours/24, hours%24, minutes%60)
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func (model appModel) statusLineValueForItem(item statusLineItem) (string, bool) {
	configuration := model.session.Configuration
	switch item {
	case statusLineItemModelWithReasoning:
		modelName := strings.TrimSpace(configuration.Model)
		if modelName == "" {
			return "", false
		}
		if effort := reasoningEffortLabel(configuration.ReasoningEffort); effort != "" {
			return modelName + " " + effort, true
		}
		return modelName, true
	case statusLineItemCurrentDir:
		currentDir := strings.TrimSpace(configuration.CWD)
		if currentDir == "" {
			return "", false
		}
		return filepath.Clean(currentDir), true
	case statusLineItemGitBranch:
		if model.workspace.Generation != model.session.Generation || model.workspace.CurrentDir != strings.TrimSpace(configuration.CWD) {
			return "", false
		}
		branch := strings.TrimSpace(model.workspace.Branch)
		return branch, branch != ""
	case statusLineItemThreadTitle:
		title := strings.TrimSpace(model.session.Title)
		return title, title != "" && title != "draft"
	case statusLineItemContextUsed:
		if model.session.ContextWindow <= 0 {
			return "", false
		}
		marker := ""
		if model.session.ContextEstimated {
			marker = "~"
		}
		return fmt.Sprintf("Context %s%d%% used", marker, model.statusLineContextUsedPercent()), true
	case statusLineItemContextWindowSize:
		if model.session.ContextWindow <= 0 {
			return "", false
		}
		return compactTokenCount(model.session.ContextWindow) + " window", true
	default:
		return "", false
	}
}

func (model appModel) statusLineContextUsedPercent() int64 {
	if model.session.ContextWindow <= 0 || model.session.ContextUsed <= 0 {
		return 0
	}
	return minInt64(100, model.session.ContextUsed*100/model.session.ContextWindow)
}

func reasoningEffortLabel(effort *llm.ReasoningEffort) string {
	if effort == nil || *effort == llm.ReasoningEffortNone {
		return "default"
	}
	return string(*effort)
}

func statusLineAccentForItem(item statusLineItem) statusLineAccent {
	switch item {
	case statusLineItemCurrentDir:
		return statusAccentPath
	case statusLineItemGitBranch:
		return statusAccentBranch
	case statusLineItemThreadTitle:
		return statusAccentThread
	case statusLineItemContextUsed, statusLineItemContextWindowSize:
		return statusAccentUsage
	case statusLineItemModelWithReasoning:
		return statusAccentModel
	default:
		return statusAccentModel
	}
}
