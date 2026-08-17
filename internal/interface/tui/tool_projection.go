package tui

import (
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

type ToolDisplayCategory string

const (
	ToolDisplayExplore ToolDisplayCategory = "explore"
	ToolDisplayCommand ToolDisplayCategory = "command"
	ToolDisplayWrite   ToolDisplayCategory = "write"
	ToolDisplayNetwork ToolDisplayCategory = "network"
	ToolDisplayGeneric ToolDisplayCategory = "generic"
)

type ToolDisplayStatus string

const (
	ToolDisplayQueued             ToolDisplayStatus = "queued"
	ToolDisplayRunning            ToolDisplayStatus = "running"
	ToolDisplayWaitingForApproval ToolDisplayStatus = "waiting_for_approval"
	ToolDisplayCompleted          ToolDisplayStatus = "completed"
	ToolDisplayFailed             ToolDisplayStatus = "failed"
	ToolDisplayDenied             ToolDisplayStatus = "denied"
	ToolDisplayPartial            ToolDisplayStatus = "partial"
)

type ToolDisplaySpec struct {
	ToolName       string
	UserFacingName string
	Category       ToolDisplayCategory
	Summary        string
	Detail         string
	ResultSummary  string
	Status         ToolDisplayStatus
	Metadata       map[string]any
}

func toolDisplaySpecFor(activity *toolActivity) ToolDisplaySpec {
	if activity == nil {
		return ToolDisplaySpec{Category: ToolDisplayGeneric, Status: ToolDisplayQueued}
	}
	return ToolDisplaySpec{
		ToolName:       activity.ToolName,
		UserFacingName: toolUserFacingName(activity.ToolName),
		Category:       toolDisplayCategoryForName(activity.ToolName),
		Summary:        activity.Title,
		Detail:         activity.Detail,
		ResultSummary:  activity.Result,
		Status:         toolDisplayStatusFor(activity),
	}
}

func toolDisplayCategoryForName(toolName string) ToolDisplayCategory {
	switch strings.TrimSpace(toolName) {
	case "read", "grep", "glob":
		return ToolDisplayExplore
	case "execute_command", "write_stdin":
		return ToolDisplayCommand
	case "write", "edit":
		return ToolDisplayWrite
	case "web_search":
		return ToolDisplayNetwork
	default:
		return ToolDisplayGeneric
	}
}

func toolUserFacingName(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "read":
		return "Read"
	case "grep":
		return "Grep"
	case "glob":
		return "Glob"
	case "execute_command":
		return "Execute command"
	case "write_stdin":
		return "Write stdin"
	case "write":
		return "Write"
	case "edit":
		return "Edit"
	case "web_search":
		return "Web search"
	default:
		return strings.TrimSpace(toolName)
	}
}

func toolDisplayStatusFor(activity *toolActivity) ToolDisplayStatus {
	if activity.PendingApproval {
		return ToolDisplayWaitingForApproval
	}
	if !activity.Completed {
		return ToolDisplayRunning
	}
	if activity.Status == protocol.ItemDeclined {
		return ToolDisplayDenied
	}
	if !activity.Success {
		return ToolDisplayFailed
	}
	if activity.Partial {
		return ToolDisplayPartial
	}
	return ToolDisplayCompleted
}
