package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
)

type toolHistoryCell struct {
	activities []*toolActivity
	byCallID   map[string]*toolActivity
	sequence   int
}

type execCell struct{ activity *toolActivity }
type exploreCell struct{ activities []*toolActivity }
type webSearchCell struct{ activity *toolActivity }

func (execCell) Kind() transcriptCellKind      { return cellTool }
func (exploreCell) Kind() transcriptCellKind   { return cellTool }
func (webSearchCell) Kind() transcriptCellKind { return cellTool }

func (cell execCell) Render(ctx transcriptRenderContext) string {
	return renderStyledToolLines(renderExecLines(cell.activity, ctx), ctx)
}
func (cell exploreCell) Render(ctx transcriptRenderContext) string {
	return renderStyledToolLines(renderExploreLines(cell.activities, ctx), ctx)
}
func (cell webSearchCell) Render(ctx transcriptRenderContext) string {
	return renderStyledToolLines(renderWebSearchLines(cell.activity, ctx), ctx)
}

func (cell execCell) RawLines() []string {
	return strings.Split(cell.Render(noColorToolContext()), "\n")
}
func (cell exploreCell) RawLines() []string {
	return strings.Split(cell.Render(noColorToolContext()), "\n")
}
func (cell webSearchCell) RawLines() []string {
	return strings.Split(cell.Render(noColorToolContext()), "\n")
}

func newToolHistoryCell() *toolHistoryCell {
	return &toolHistoryCell{byCallID: map[string]*toolActivity{}}
}
func (*toolHistoryCell) Kind() transcriptCellKind { return cellTool }
func (cell *toolHistoryCell) IsComplete() bool {
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
func (cell *toolHistoryCell) Apply(item event.Event) bool {
	if cell == nil {
		return false
	}
	switch item := item.(type) {
	case event.ToolCallStarted:
		if _, exists := cell.byCallID[item.CallID]; exists {
			return true
		}
		cell.sequence++
		activity := activityFromStarted(item, cell.sequence)
		cell.activities = append(cell.activities, activity)
		cell.byCallID[item.CallID] = activity
		return true
	case event.ToolCallCompleted:
		activity := cell.byCallID[item.CallID]
		if activity == nil {
			return false
		}
		activity.Result = strings.TrimSpace(item.Summary)
		activity.Duration = item.Duration
		activity.Success = item.Success
		activity.Partial = item.Partial
		activity.Completed = true
		return true
	default:
		return false
	}
}
func (cell *toolHistoryCell) Complete() transcriptCell {
	if cell == nil {
		return nil
	}
	copyCell := &toolHistoryCell{activities: append([]*toolActivity(nil), cell.activities...), byCallID: map[string]*toolActivity{}, sequence: cell.sequence}
	return copyCell
}
func (cell *toolHistoryCell) RawLines() []string {
	if cell == nil {
		return nil
	}
	return strings.Split(cell.renderPlain(), "\n")
}
func (cell *toolHistoryCell) Render(ctx transcriptRenderContext) string {
	if cell == nil {
		return ""
	}
	return renderTranscriptCells(cell.projections(), ctx)
}

func (cell *toolHistoryCell) projections() []transcriptCell {
	activities := append([]*toolActivity(nil), cell.activities...)
	sort.SliceStable(activities, func(i, j int) bool { return activities[i].Sequence < activities[j].Sequence })
	var projections []transcriptCell
	var explored []*toolActivity
	flushExplored := func() {
		if len(explored) == 0 {
			return
		}
		projections = append(projections, exploreCell{activities: append([]*toolActivity(nil), explored...)})
		explored = nil
	}
	for _, activity := range activities {
		if activity.Kind == activityExplore {
			explored = append(explored, activity)
			continue
		}
		flushExplored()
		if activity.Kind == activityNetwork {
			projections = append(projections, webSearchCell{activity: activity})
			continue
		}
		projections = append(projections, execCell{activity: activity})
	}
	flushExplored()
	return projections
}

func (cell *toolHistoryCell) renderPlain() string {
	return cell.Render(noColorToolContext())
}

func noColorToolContext() transcriptRenderContext {
	now := time.Now()
	return transcriptRenderContext{Width: 120, Palette: terminalPalette{NoColor: true, Level: colorLevelNone}, Now: now, MotionStart: now, Motion: motionReduced}
}

func renderExploreLines(activities []*toolActivity, ctx transcriptRenderContext) []styledLine {
	complete := true
	success := true
	for _, activity := range activities {
		complete = complete && activity.Completed
		success = success && activityCompletedSuccessfully(activity)
	}
	markerStyle := styleDim
	if complete && success {
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
		if activity.Completed && !activity.Success {
			line = append(line, styledSpan{Text: " · failed", Style: styleFailure})
		}
		lines = append(lines, line)
		itemIndex++
	}
	return lines
}

func exploreVerbAndDetail(activity *toolActivity) (string, string) {
	value := strings.TrimSpace(activity.Title)
	for _, verb := range []string{"Read", "List", "Search"} {
		if strings.HasPrefix(strings.ToLower(value), strings.ToLower(verb)) {
			return verb, strings.TrimSpace(value[len(verb):])
		}
	}
	return "Read", value
}

func renderExecLines(activity *toolActivity, ctx transcriptRenderContext) []styledLine {
	complete := activity.Completed
	markerStyle := styleDim
	if complete && activityCompletedSuccessfully(activity) {
		markerStyle = styleSuccess
	} else if complete {
		markerStyle = styleFailure
	}
	title := "Running"
	if complete {
		title = "Ran"
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(activity.Title)), "you ran") {
		title = "You ran"
	}
	commandLines := strings.Split(strings.TrimSpace(firstNonEmpty(activity.Detail, activity.Title)), "\n")
	if len(commandLines) > 2 {
		commandLines = append(commandLines[:2], "…")
	}
	header := styledLine{markerSpan(complete, activityCompletedSuccessfully(activity), ctx, markerStyle), {Text: " "}, {Text: title, Style: styleBold}}
	if len(commandLines) > 0 && commandLines[0] != "" {
		header = append(header, styledSpan{Text: " " + commandLines[0], Style: styleAccent})
	}
	lines := []styledLine{header}
	for _, command := range commandLines[1:] {
		lines = append(lines, styledLine{{Text: "  │ ", Style: styleDim}, {Text: command, Style: styleAccent}})
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
			style := stylePlain
			if output == "(no output)" || strings.HasPrefix(output, "… +") {
				style = styleDim
			}
			lines = append(lines, styledLine{{Text: prefix, Style: styleDim}, {Text: output, Style: style}})
		}
	}
	return lines
}

func renderWebSearchLines(activity *toolActivity, ctx transcriptRenderContext) []styledLine {
	complete := activity.Completed
	success := activityCompletedSuccessfully(activity)
	markerStyle := styleDim
	if complete && success {
		markerStyle = styleSuccess
	} else if complete {
		markerStyle = styleFailure
	}
	title := "Searching the web"
	if complete {
		title = "Searched the web"
	}
	lines := []styledLine{{markerSpan(complete, success, ctx, markerStyle), {Text: " "}, {Text: title, Style: styleBold}}}
	detail := firstNonEmpty(activity.Detail, activity.Title)
	if detail != "" {
		lines = append(lines, styledLine{{Text: "  └ ", Style: styleDim}, {Text: detail, Style: styleAccent}})
	}
	return lines
}

func markerSpan(complete, success bool, ctx transcriptRenderContext, completedStyle semanticStyle) styledSpan {
	if !complete {
		return styledSpan{Text: activityIndicator(ctx.Now, ctx.MotionStart, ctx.Motion, ctx.Palette), Style: stylePlain}
	}
	return styledSpan{Text: "•", Style: completedStyle}
}

func renderStyledToolLines(lines []styledLine, ctx transcriptRenderContext) string {
	cell := styledTranscriptCell{kind: cellTool, lines: lines}
	return cell.Render(ctx)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
