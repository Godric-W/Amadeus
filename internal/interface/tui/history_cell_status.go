package tui

import (
	"fmt"
	"strings"

	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type StatusHistoryCell struct {
	Snapshot application.StatusSnapshot
}

func NewStatusHistoryCell(snapshot application.StatusSnapshot) HistoryCell {
	return StatusHistoryCell{Snapshot: snapshot}
}

func (cell StatusHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	lines := []styledLine{{{Text: "• ", Style: styleDim}, {Text: "Session Status", Style: styleBold}}}
	for _, value := range cell.values() {
		lines = append(lines, styledLine{{Text: "  " + value, Style: styleDim}})
	}
	return lines
}

func (cell StatusHistoryCell) RawLines() []string {
	return append([]string{"Session Status"}, cell.values()...)
}

func (StatusHistoryCell) IsStreamContinuation() bool { return false }

func (cell StatusHistoryCell) values() []string {
	snapshot := cell.Snapshot
	values := []string{
		fmt.Sprintf("Thread: %s", displayValue(string(snapshot.ThreadID))),
		fmt.Sprintf("Title: %s", displayValue(snapshot.Title)),
		fmt.Sprintf("Current directory: %s", displayValue(snapshot.CurrentDir)),
		fmt.Sprintf("Model: %s / %s", displayValue(snapshot.Provider), displayValue(snapshot.Model)),
		fmt.Sprintf("Reasoning effort: %s", displayReasoningEffort(snapshot.ReasoningEffort)),
		fmt.Sprintf("Mode: %s · Phase: %s", displayValue(string(snapshot.Mode)), displayValue(snapshot.Phase)),
		fmt.Sprintf("Tokens: %d input · %d output · %d total / %d context", snapshot.Usage.InputTokens, snapshot.Usage.OutputTokens, snapshot.Usage.TotalTokens, snapshot.ContextWindow),
		fmt.Sprintf("Rollout items: %d · Permission grants: %d", snapshot.RolloutItems, snapshot.PermissionGrantCount),
	}
	if strings.TrimSpace(snapshot.SkillRevision) != "" || strings.TrimSpace(snapshot.MCPRevision) != "" {
		values = append(values, fmt.Sprintf("Capabilities: skills %s · MCP %s", displayValue(snapshot.SkillRevision), displayValue(snapshot.MCPRevision)))
	}
	return values
}

func displayReasoningEffort(effort *llm.ReasoningEffort) string {
	if effort == nil {
		return "provider default"
	}
	return string(*effort)
}

func displayValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return strings.TrimSpace(value)
}
