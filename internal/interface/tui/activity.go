package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Godric-W/Amadeus/internal/agent/event"
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

func activityFromStarted(item event.ToolCallStarted, sequence int) *toolActivity {
	title := strings.TrimSpace(item.ActionSummary)
	if title == "" {
		title = strings.TrimSpace(item.ToolName)
	}
	return &toolActivity{
		CallID: item.CallID, Iteration: item.Iteration, Sequence: sequence,
		Kind: activityKindFromSideEffect(item.SideEffect), Title: title, Detail: strings.TrimSpace(item.Detail),
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
