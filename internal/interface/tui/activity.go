package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type toolActivity struct {
	CallID          string
	ToolName        string
	Iteration       int
	Sequence        int
	Title           string
	Detail          string
	Result          string
	Duration        time.Duration
	Success         bool
	Status          protocol.ItemStatus
	Partial         bool
	PendingApproval bool
	Completed       bool
	DetailAvailable bool
	ResultDetailID  string
	ToolResult      *tool.ToolResult
}

func activityFromStarted(item protocol.TurnItem, sequence int) *toolActivity {
	payload, _ := item.Payload.(map[string]any)
	title, _ := payload["action_summary"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		title = strings.TrimSpace(item.ToolName)
	}
	detail, _ := payload["detail"].(string)
	return &toolActivity{
		CallID: item.CallID, ToolName: strings.TrimSpace(item.ToolName), Sequence: sequence,
		Status: protocol.ItemInProgress,
		Title:  title, Detail: strings.TrimSpace(detail),
	}
}

func activityCompletedSuccessfully(activity *toolActivity) bool {
	return activity != nil && activity.Completed && activity.Success && !activity.Partial
}

func activityMarkerStyle(activity *toolActivity) semanticStyle {
	if activity == nil || !activity.Completed {
		return styleDim
	}
	if activity.Partial && activity.Success {
		return styleAccent
	}
	if activity.Success {
		return styleSuccess
	}
	return styleFailure
}

func activityStatusLabel(activity *toolActivity) string {
	if activity == nil {
		return ""
	}
	if activity.Partial && activity.Success {
		return "partial"
	}
	switch activity.Status {
	case protocol.ItemDeclined:
		return "denied"
	case protocol.ItemFailed:
		return "failed"
	default:
		return ""
	}
}

func summarizeActivityText(value string, maximumRunes, maximumLines int) string {
	value = sanitizeFullscreenContent(value)
	lines := strings.Split(value, "\n")
	if maximumLines > 0 && len(lines) > maximumLines {
		hidden := len(lines) - maximumLines
		lines = append(lines[:maximumLines], fmt.Sprintf("… +%d lines", hidden))
	}
	value = strings.TrimSpace(strings.Join(lines, "\n"))
	if maximumRunes > 0 && utf8.RuneCountInString(value) > maximumRunes {
		runes := []rune(value)
		value = strings.TrimSpace(string(runes[:maximumRunes-1])) + "…"
	}
	return value
}
