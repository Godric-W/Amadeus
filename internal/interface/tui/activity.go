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

type iterationActivity struct {
	Iteration int
	CallIDs   []string
	Tools     map[string]*toolActivity
}

func newIterationActivity(iteration int) *iterationActivity {
	return &iterationActivity{Iteration: iteration, Tools: map[string]*toolActivity{}}
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

func renderIterationActivities(iteration *iterationActivity) []fullscreenEntry {
	if iteration == nil {
		return nil
	}
	activities := make([]*toolActivity, 0, len(iteration.CallIDs))
	for _, callID := range iteration.CallIDs {
		if candidate := iteration.Tools[callID]; candidate != nil {
			activities = append(activities, candidate)
		}
	}
	entries := make([]fullscreenEntry, 0, len(activities)+2)
	var explored []*toolActivity
	for _, activity := range activities {
		if activity.Kind == activityExplore {
			explored = append(explored, activity)
			continue
		}
		entries = append(entries, fullscreenEntry{kind: "activity", content: renderActionActivity(activity), successful: activityCompletedSuccessfully(activity)})
	}
	if len(explored) > 0 {
		entries = append([]fullscreenEntry{{kind: "activity", content: renderExploredActivities(explored), successful: activitiesCompletedSuccessfully(explored)}}, entries...)
	}
	if len(entries) > 0 {
		entries = append(entries, fullscreenEntry{kind: "separator"})
	}
	return entries
}

func activityCompletedSuccessfully(activity *toolActivity) bool {
	return activity != nil && activity.Completed && activity.Success && !activity.Partial
}

func activitiesCompletedSuccessfully(activities []*toolActivity) bool {
	if len(activities) == 0 {
		return false
	}
	for _, activity := range activities {
		if !activityCompletedSuccessfully(activity) {
			return false
		}
	}
	return true
}

func renderExploredActivities(activities []*toolActivity) string {
	lines := []string{"• Explored"}
	for index, activity := range activities {
		prefix := "    "
		if index == 0 {
			prefix = "  └ "
		}
		line := prefix + activity.Title
		if !activity.Success && activity.Completed {
			line += " · failed"
		} else if activity.Partial {
			line += " · partial"
		}
		lines = append(lines, line)
		if !activity.Success && activity.Completed && activity.Result != "" {
			lines = append(lines, "    └ "+summarizeActivityText(activity.Result, 180, 1))
		}
	}
	return strings.Join(lines, "\n")
}

func renderActionActivity(activity *toolActivity) string {
	if activity == nil {
		return ""
	}
	lines := []string{"• " + activity.Title}
	if activity.Detail != "" {
		lines = append(lines, "  │ "+activity.Detail)
	}
	result := summarizeActivityText(activity.Result, 600, 5)
	if activity.DetailAvailable && result != "" && result != strings.TrimSpace(sanitizeFullscreenContent(activity.Result)) {
		result = strings.TrimSpace(result) + " (ctrl + t to view transcript)"
	}
	if result != "" {
		resultLines := strings.Split(result, "\n")
		for index, line := range resultLines {
			prefix := "  │ "
			if index == len(resultLines)-1 {
				prefix = "  └ "
			}
			lines = append(lines, prefix+line)
		}
	} else if activity.Completed {
		state := "completed"
		if !activity.Success {
			state = "failed"
		}
		if activity.Partial {
			state += " · partial"
		}
		if activity.Duration > 0 {
			state += " · " + activity.Duration.Round(time.Millisecond).String()
		}
		lines = append(lines, "  └ "+state)
	}
	return strings.Join(lines, "\n")
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
