package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/filechange"
)

type FileChangeCell struct{ activity *toolActivity }
type GenericToolCell struct{ activity *toolActivity }

func (cell FileChangeCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	return renderFileChangeLines(cell.activity, ctx)
}

func (cell FileChangeCell) RawLines() []string {
	return rawStyledLines(renderFileChangeLines(cell.activity, rawToolContext()))
}

func (FileChangeCell) IsStreamContinuation() bool { return false }

func (cell GenericToolCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	return renderGenericToolLines(cell.activity, ctx)
}

func (cell GenericToolCell) RawLines() []string {
	return rawStyledLines(renderGenericToolLines(cell.activity, rawToolContext()))
}

func (GenericToolCell) IsStreamContinuation() bool { return false }

func renderFileChangeLines(activity *toolActivity, ctx HistoryRenderContext) []styledLine {
	if activity == nil {
		return nil
	}
	preview := activityFileChangePreview(activity)
	path := trimPresentationPrefix(activity.Title, fileChangePresentationPrefix(activity.ToolName))
	if path == activity.ToolName {
		path = ""
	}
	if path == "" && activity.ToolResult != nil {
		path = activity.ToolResult.Display.Title
	}
	if path == "" && preview != nil {
		path = preview.Path
	}

	complete := activity.Completed
	success := activity.Success
	markerStyle := activityMarkerStyle(activity)
	marker := markerSpan(complete, success, ctx, markerStyle)
	operation := fileChangeOperation(activity, preview)
	verb := operation
	if !complete {
		verb = fileChangeProgressVerb(activity.ToolName, preview)
		if activity.PendingApproval {
			verb = "Waiting for approval"
		}
	}
	header := styledLine{marker, {Text: " "}, {Text: verb, Style: styleBold}}
	if path != "" {
		header = append(header, styledSpan{Text: " " + path, Style: stylePlain})
	}
	if label := activityStatusLabel(activity); complete && label != "" {
		style := styleFailure
		if label == "partial" {
			style = styleAccent
		}
		header = append(header, styledSpan{Text: " · " + label, Style: style})
	}
	lines := []styledLine{header}
	if preview != nil && complete && activity.Success {
		stats := preview.Stats
		lines = append(lines, styledLine{{Text: "  └ ", Style: styleDim}, {Text: fmt.Sprintf("+%d -%d lines", stats.Insertions, stats.Deletions), Style: styleDim}})
	} else if complete && activity.Success {
		lines = append(lines, styledLine{{Text: "  └ ", Style: styleDim}, {Text: "updated", Style: styleDim}})
	}
	return lines
}

func renderGenericToolLines(activity *toolActivity, ctx HistoryRenderContext) []styledLine {
	if activity == nil {
		return nil
	}
	complete := activity.Completed
	success := activity.Success
	markerStyle := activityMarkerStyle(activity)
	spec := toolDisplaySpecFor(activity)
	name := firstNonEmpty(spec.UserFacingName, activity.ToolName, "tool")
	state := "Running"
	if activity.PendingApproval {
		state = "Waiting for approval"
	}
	if complete {
		state = "Ran"
	}
	header := styledLine{markerSpan(complete, success, ctx, markerStyle), {Text: " "}, {Text: state, Style: styleBold}, {Text: " " + name, Style: styleAccent}}
	detail := firstNonEmpty(activity.Detail, activity.Title)
	if detail != "" {
		header = append(header, styledSpan{Text: " " + detail, Style: stylePlain})
	}
	if label := activityStatusLabel(activity); complete && label != "" {
		style := styleFailure
		if label == "partial" {
			style = styleAccent
		}
		header = append(header, styledSpan{Text: " · " + label, Style: style})
	}
	return []styledLine{header}
}

func activityFileChangePreview(activity *toolActivity) *filechange.Preview {
	if activity == nil || activity.ToolResult == nil {
		return nil
	}
	for _, value := range []any{activity.ToolResult.Display.Data, activity.ToolResult.Data} {
		if preview := decodeFileChangePreview(value); preview != nil {
			return preview
		}
		var result filechange.Result
		if decodeJSON(value, &result) && result.Diff != nil {
			preview := result.Diff.Clone()
			return &preview
		}
	}
	return nil
}

func toolActivityDetailContent(activity *toolActivity, fallback string) string {
	if activity == nil {
		return strings.TrimSpace(fallback)
	}
	parts := []string{activity.Detail, fallback}
	if preview := activityFileChangePreview(activity); preview != nil {
		parts = append(parts, preview.UnifiedDiff)
	}
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return strings.TrimSpace(strings.Join(values, "\n\n"))
}

func decodeFileChangePreview(value any) *filechange.Preview {
	switch typed := value.(type) {
	case filechange.Preview:
		preview := typed.Clone()
		return &preview
	case *filechange.Preview:
		if typed == nil {
			return nil
		}
		preview := typed.Clone()
		return &preview
	}
	var preview filechange.Preview
	if !decodeJSON(value, &preview) || strings.TrimSpace(preview.Path) == "" || (preview.UnifiedDiff == "" && preview.AfterHash == "" && len(preview.Hunks) == 0) {
		return nil
	}
	return &preview
}

func decodeJSON(value any, target any) bool {
	if value == nil {
		return false
	}
	bytes, err := json.Marshal(value)
	if err != nil || len(bytes) == 0 {
		return false
	}
	return json.Unmarshal(bytes, target) == nil
}

func fileChangeOperation(activity *toolActivity, preview *filechange.Preview) string {
	if preview != nil && preview.Operation == filechange.OperationCreate {
		return "Created"
	}
	if activity.ToolName == "write" && preview == nil {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(activity.Title)), "create") {
			return "Created"
		}
		return "Wrote"
	}
	return "Updated"
}

func fileChangeProgressVerb(toolName string, preview *filechange.Preview) string {
	if preview != nil && preview.Operation == filechange.OperationCreate {
		return "Creating"
	}
	if toolName == "write" && preview == nil {
		return "Writing"
	}
	return "Updating"
}

func fileChangePresentationPrefix(toolName string) string {
	if toolName == "write" {
		return "Create"
	}
	return "Update"
}
