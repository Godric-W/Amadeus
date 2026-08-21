package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type ToolHistoryCell struct {
	activities []*toolActivity
	byCallID   map[string]*toolActivity
	sequence   int
}

type ExecCell struct{ activity *toolActivity }
type ExploreCell struct{ activities []*toolActivity }
type WebSearchCell struct{ activity *toolActivity }
type WebFetchCell struct{ activity *toolActivity }

func (cell ExecCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	return renderExecLines(cell.activity, ctx)
}
func (cell ExploreCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	return renderExploreLines(cell.activities, ctx)
}
func (cell WebSearchCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	return renderWebSearchLines(cell.activity, ctx)
}
func (cell WebFetchCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	return renderWebFetchLines(cell.activity, ctx)
}

func (cell ExecCell) RawLines() []string {
	return rawStyledLines(renderExecLines(cell.activity, rawToolContext()))
}
func (cell ExploreCell) RawLines() []string {
	return rawStyledLines(renderExploreLines(cell.activities, rawToolContext()))
}
func (cell WebSearchCell) RawLines() []string {
	return rawStyledLines(renderWebSearchLines(cell.activity, rawToolContext()))
}
func (cell WebFetchCell) RawLines() []string {
	return rawStyledLines(renderWebFetchLines(cell.activity, rawToolContext()))
}

func (ExecCell) IsStreamContinuation() bool      { return false }
func (ExploreCell) IsStreamContinuation() bool   { return false }
func (WebSearchCell) IsStreamContinuation() bool { return false }
func (WebFetchCell) IsStreamContinuation() bool  { return false }

func newToolHistoryCell() *ToolHistoryCell {
	return &ToolHistoryCell{byCallID: map[string]*toolActivity{}}
}
func (cell *ToolHistoryCell) IsComplete() bool {
	if cell == nil || len(cell.activities) == 0 {
		return false
	}
	for _, activity := range cell.activities {
		if !activity.Completed {
			return false
		}
	}
	return true
}

func (cell *ToolHistoryCell) SetApprovalState(callID string, pending bool) bool {
	if cell == nil {
		return false
	}
	activity := cell.byCallID[strings.TrimSpace(callID)]
	if activity == nil || activity.PendingApproval == pending {
		return false
	}
	activity.PendingApproval = pending
	return true
}
func (cell *ToolHistoryCell) Apply(message protocol.EventMsg) bool {
	if cell == nil {
		return false
	}
	switch item := message.(type) {
	case protocol.ItemStartedEvent:
		if item.Item.ToolName == "" {
			return false
		}
		if _, exists := cell.byCallID[item.Item.CallID]; exists {
			return true
		}
		cell.sequence++
		activity := activityFromStarted(item.Item, cell.sequence)
		cell.activities = append(cell.activities, activity)
		cell.byCallID[item.Item.CallID] = activity
		return true
	case protocol.ItemCompletedEvent:
		if item.Item.ToolName == "" {
			return false
		}
		activity := cell.byCallID[item.Item.CallID]
		if activity == nil {
			return false
		}
		activity.Result = strings.TrimSpace(item.Item.Text)
		if item.Item.ToolResult != nil {
			activity.Result = displayResultText(*item.Item.ToolResult)
			result := item.Item.ToolResult.Clone()
			activity.ToolResult = &result
		}
		activity.Status = item.Item.Status
		activity.Success = item.Item.Status == protocol.ItemStatusCompleted
		if payload, ok := item.Item.Payload.(map[string]any); ok {
			if duration, ok := payload["duration"].(string); ok {
				activity.Duration, _ = time.ParseDuration(duration)
			}
			activity.Partial, _ = payload["partial"].(bool)
		}
		activity.Completed = true
		return true
	default:
		return false
	}
}

func displayResultText(result tool.ToolResult) string {
	if result.Display.Kind == tool.ToolDisplayProcess && strings.TrimSpace(result.Text) != "" {
		return strings.TrimSpace(result.Text)
	}
	if summary := strings.TrimSpace(result.Display.Summary); summary != "" {
		return summary
	}
	return strings.TrimSpace(result.Text)
}
func (cell *ToolHistoryCell) Complete() HistoryCell {
	if cell == nil {
		return nil
	}
	copyCell := &ToolHistoryCell{activities: cloneToolActivities(cell.activities), byCallID: map[string]*toolActivity{}, sequence: cell.sequence}
	return copyCell
}
func (cell *ToolHistoryCell) RawLines() []string {
	if cell == nil {
		return nil
	}
	return rawStyledLines(cell.linesForMode(HistoryRenderRaw, rawToolContext()))
}
func (cell *ToolHistoryCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	if cell == nil {
		return nil
	}
	return cell.linesForMode(HistoryRenderRich, ctx)
}
func (*ToolHistoryCell) IsStreamContinuation() bool { return false }

func (cell *ToolHistoryCell) projections() []HistoryCell {
	activities := append([]*toolActivity(nil), cell.activities...)
	sort.SliceStable(activities, func(i, j int) bool { return activities[i].Sequence < activities[j].Sequence })
	var projections []HistoryCell
	var explored []*toolActivity
	flushExplored := func() {
		if len(explored) == 0 {
			return
		}
		projections = append(projections, ExploreCell{activities: append([]*toolActivity(nil), explored...)})
		explored = nil
	}
	for _, activity := range activities {
		spec := toolDisplaySpecFor(activity)
		if spec.Category == ToolDisplayExplore {
			explored = append(explored, activity)
			continue
		}
		flushExplored()
		if spec.Category == ToolDisplayNetwork {
			switch activity.ToolName {
			case "web_search":
				projections = append(projections, WebSearchCell{activity: activity})
			case "web_fetch":
				projections = append(projections, WebFetchCell{activity: activity})
			default:
				projections = append(projections, GenericToolCell{activity: activity})
			}
			continue
		}
		switch spec.Category {
		case ToolDisplayCommand:
			projections = append(projections, ExecCell{activity: activity})
		case ToolDisplayWrite:
			projections = append(projections, FileChangeCell{activity: activity})
		default:
			projections = append(projections, GenericToolCell{activity: activity})
		}
	}
	flushExplored()
	return projections
}

func (cell *ToolHistoryCell) linesForMode(mode HistoryRenderMode, ctx HistoryRenderContext) []styledLine {
	var lines []styledLine
	hasVisible := false
	for _, projection := range cell.projections() {
		projectionLines := historyLinesForMode(projection, mode, ctx)
		if len(projectionLines) == 0 {
			continue
		}
		if hasVisible {
			lines = append(lines, styledLine{})
		}
		lines = append(lines, projectionLines...)
		hasVisible = true
	}
	return lines
}

func rawToolContext() HistoryRenderContext {
	return HistoryRenderContext{Width: 120, Palette: terminalPalette{NoColor: true, Level: colorLevelNone}, Motion: motionReduced}
}

func renderExploreLines(activities []*toolActivity, ctx HistoryRenderContext) []styledLine {
	complete := true
	success := true
	partial := false
	for _, activity := range activities {
		complete = complete && activity.Completed
		success = success && activity.Success
		partial = partial || activity.Partial
	}
	markerStyle := styleDim
	if complete && success && partial {
		markerStyle = styleAccent
	} else if complete && success {
		markerStyle = styleSuccess
	} else if complete && !success {
		markerStyle = styleFailure
	}
	title := "Exploring"
	if complete {
		title = "Explored"
	}
	lines := []styledLine{{markerSpan(complete, success, ctx, markerStyle), {Text: " "}, {Text: title, Style: styleBold}}}
	seenReads := map[string]struct{}{}
	itemIndex := 0
	for _, activity := range activities {
		verb, detail := exploreVerbAndDetail(activity)
		key := verb + "\x00" + detail
		if verb == "Read" {
			if _, seen := seenReads[key]; seen {
				continue
			}
			seenReads[key] = struct{}{}
		}
		prefix := "    "
		if itemIndex == 0 {
			prefix = "  └ "
		}
		line := styledLine{{Text: prefix, Style: styleDim}, {Text: verb, Style: styleAccent}}
		if detail != "" {
			line = append(line, styledSpan{Text: " " + detail, Style: stylePlain})
		}
		if label := activityStatusLabel(activity); activity.Completed && label != "" {
			style := styleFailure
			if label == "partial" {
				style = styleAccent
			}
			line = append(line, styledSpan{Text: " · " + label, Style: style})
		}
		lines = append(lines, line)
		itemIndex++
	}
	return lines
}

func exploreVerbAndDetail(activity *toolActivity) (string, string) {
	value := strings.TrimSpace(activity.Title)
	switch activity.ToolName {
	case "read":
		return "Read", trimPresentationPrefix(value, "Read")
	case "grep":
		return "Grep", trimPresentationPrefix(value, "Search")
	case "glob":
		return "Glob", trimPresentationPrefix(value, "Find")
	default:
		for _, verb := range []string{"Read", "List", "Search"} {
			if strings.HasPrefix(strings.ToLower(value), strings.ToLower(verb)) {
				return verb, strings.TrimSpace(value[len(verb):])
			}
		}
		return firstNonEmpty(activity.ToolName, "Tool"), value
	}
}

func isExploreTool(toolName string) bool {
	return toolDisplayCategoryForName(toolName) == ToolDisplayExplore
}

func trimPresentationPrefix(value, prefix string) string {
	if strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix)) {
		return strings.TrimSpace(value[len(prefix):])
	}
	return value
}

func renderExecLines(activity *toolActivity, ctx HistoryRenderContext) []styledLine {
	complete := activity.Completed
	markerStyle := activityMarkerStyle(activity)
	title := "Running"
	if activity.PendingApproval {
		title = "Waiting for approval"
	}
	if complete {
		title = "Ran"
	}
	if !activity.PendingApproval && strings.HasPrefix(strings.ToLower(strings.TrimSpace(activity.Title)), "you ran") {
		title = "You ran"
	}
	commandLines := strings.Split(commandActivityDetail(activity), "\n")
	if len(commandLines) > 2 {
		commandLines = append(commandLines[:2], "…")
	}
	header := styledLine{markerSpan(complete, activity.Success, ctx, markerStyle), {Text: " "}, {Text: title, Style: styleBold}}
	if len(commandLines) > 0 && commandLines[0] != "" {
		header = append(header, styledSpan{Text: " " + commandLines[0], Style: stylePlain})
	}
	if label := activityStatusLabel(activity); complete && label != "" {
		style := styleFailure
		if label == "partial" {
			style = styleAccent
		}
		header = append(header, styledSpan{Text: " · " + label, Style: style})
	}
	lines := []styledLine{header}
	for _, command := range commandLines[1:] {
		lines = append(lines, styledLine{{Text: "  │ ", Style: styleDim}, {Text: command, Style: stylePlain}})
	}
	result := summarizeActivityText(activity.Result, 1200, 5)
	if complete && result == "" {
		result = "(no output)"
	}
	if result != "" {
		for index, output := range strings.Split(result, "\n") {
			prefix := "    "
			if index == 0 {
				prefix = "  └ "
			}
			style := styleDim
			if output == "(no output)" || strings.HasPrefix(output, "… +") {
				style = styleDim
			}
			lines = append(lines, styledLine{{Text: prefix, Style: styleDim}, {Text: output, Style: style}})
		}
	}
	return lines
}

func commandActivityDetail(activity *toolActivity) string {
	if activity == nil {
		return ""
	}
	if detail := strings.TrimSpace(activity.Detail); detail != "" {
		return detail
	}
	value := strings.TrimSpace(activity.Title)
	for _, prefix := range []string{"You ran", "Ran", "Run"} {
		if strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix)) {
			return strings.TrimSpace(value[len(prefix):])
		}
	}
	return value
}

func renderWebSearchLines(activity *toolActivity, ctx HistoryRenderContext) []styledLine {
	return renderWebActivityLines(activity, ctx, "Searching the web", "Searched the web")
}

func renderWebFetchLines(activity *toolActivity, ctx HistoryRenderContext) []styledLine {
	return renderWebActivityLines(activity, ctx, "Fetching web content", "Fetched web content")
}

func renderWebActivityLines(activity *toolActivity, ctx HistoryRenderContext, runningTitle, completedTitle string) []styledLine {
	complete := activity.Completed
	success := activity.Success
	markerStyle := activityMarkerStyle(activity)
	title := runningTitle
	if complete {
		title = completedTitle
	}
	lines := []styledLine{{markerSpan(complete, success, ctx, markerStyle), {Text: " "}, {Text: title, Style: styleBold}}}
	if label := activityStatusLabel(activity); complete && label != "" {
		style := styleFailure
		if label == "partial" {
			style = styleAccent
		}
		lines[0] = append(lines[0], styledSpan{Text: " · " + label, Style: style})
	}
	detail := firstNonEmpty(activity.Detail, activity.Title)
	if detail != "" {
		lines = append(lines, styledLine{{Text: "  └ ", Style: styleDim}, {Text: detail, Style: styleAccent}})
	}
	return lines
}

func markerSpan(complete, success bool, ctx HistoryRenderContext, completedStyle semanticStyle) styledSpan {
	if !complete {
		return styledSpan{Text: activityIndicator(ctx.Now, ctx.MotionStart, ctx.Motion, ctx.Palette), Style: stylePlain}
	}
	return styledSpan{Text: "•", Style: completedStyle}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cloneToolActivities(activities []*toolActivity) []*toolActivity {
	result := make([]*toolActivity, 0, len(activities))
	for _, activity := range activities {
		if activity == nil {
			continue
		}
		copyActivity := *activity
		if activity.ToolResult != nil {
			result := activity.ToolResult.Clone()
			copyActivity.ToolResult = &result
		}
		result = append(result, &copyActivity)
	}
	return result
}
