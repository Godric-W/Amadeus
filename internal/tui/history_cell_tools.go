package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
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
type ViewImageCell struct{ activity *toolActivity }

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
func (cell ViewImageCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	return renderViewImageLines(cell.activity, ctx)
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
func (cell ViewImageCell) RawLines() []string {
	return rawStyledLines(renderViewImageLines(cell.activity, rawToolContext()))
}

func (ExecCell) IsStreamContinuation() bool      { return false }
func (ExploreCell) IsStreamContinuation() bool   { return false }
func (WebSearchCell) IsStreamContinuation() bool { return false }
func (WebFetchCell) IsStreamContinuation() bool  { return false }
func (ViewImageCell) IsStreamContinuation() bool { return false }

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
		_, _, durationMS, partial := toolItemPayloadValues(item.Item.Payload)
		activity.Duration = time.Duration(durationMS) * time.Millisecond
		activity.Partial = partial
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
func (*ToolHistoryCell) IsStreamContinuation() bool    { return false }
func (*ToolHistoryCell) HistoryBoundaryBlankRows() int { return 1 }

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
		if activity.ToolName == "view_image" {
			flushExplored()
			projections = append(projections, ViewImageCell{activity: activity})
			continue
		}
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
