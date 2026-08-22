package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
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

func (model *fullscreenModel) refreshStatusLine() {
	segments := make([]statusLineSegment, 0, len(fixedStatusLineItems))
	for _, item := range fixedStatusLineItems {
		if value, ok := model.statusLineValueForItem(item); ok {
			segments = append(segments, statusLineSegment{Item: item, Text: value})
		}
	}
	model.footer.StatusLine = statusLineState{Segments: segments, ContextUsedPercent: model.statusLineContextUsedPercent()}
	model.footer.CollaborationIndicator = collaborationModeIndicatorFor(model.session.mode())
}

func (model fullscreenModel) statusLineValueForItem(item statusLineItem) (string, bool) {
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
		return fmt.Sprintf("Context %d%% used", model.statusLineContextUsedPercent()), true
	case statusLineItemContextWindowSize:
		if model.session.ContextWindow <= 0 {
			return "", false
		}
		return compactTokenCount(model.session.ContextWindow) + " window", true
	default:
		return "", false
	}
}

func (model fullscreenModel) statusLineContextUsedPercent() int64 {
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
