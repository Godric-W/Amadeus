package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

type activityKind string

const (
	activityExplore activityKind = "explore"
	activityRun     activityKind = "run"
	activityNetwork activityKind = "network"
)

type toolActivity struct {
	CallID          string
	Iteration       int
	Sequence        int
	Kind            activityKind
	Title           string
	Detail          string
	Result          string
	Duration        time.Duration
	Success         bool
	Partial         bool
	Completed       bool
	DetailAvailable bool
	ResultDetailID  string
}

func activityFromStarted(item protocol.TurnItem, sequence int) *toolActivity {
	payload, _ := item.Payload.(map[string]any)
	title, _ := payload["action_summary"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		title = strings.TrimSpace(item.ToolName)
	}
	detail, _ := payload["detail"].(string)
	sideEffect, _ := payload["side_effect"].(string)
	return &toolActivity{
		CallID: item.CallID, Sequence: sequence,
		Kind: activityKindFromSideEffect(sideEffect), Title: title, Detail: strings.TrimSpace(detail),
	}
}

func activityKindFromSideEffect(sideEffect string) activityKind {
	switch strings.TrimSpace(sideEffect) {
	case "none", "read", "":
		return activityExplore
	case "network":
		return activityNetwork
	default:
		return activityRun
	}
}

func activityCompletedSuccessfully(activity *toolActivity) bool {
	return activity != nil && activity.Completed && activity.Success && !activity.Partial
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
