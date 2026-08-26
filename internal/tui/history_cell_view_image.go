package tui

import (
	"fmt"
	"strings"
)

func renderViewImageLines(activity *toolActivity, ctx HistoryRenderContext) []styledLine {
	if activity == nil {
		return nil
	}
	title := "Viewing image"
	if activity.PendingApproval {
		title = "Waiting to view image"
	} else if activity.Completed {
		if activity.Success {
			title = "Viewed image"
		} else {
			title = "Failed to view image"
		}
	}
	header := styledLine{markerSpan(activity.Completed, activity.Success, ctx, activityMarkerStyle(activity)), {Text: " "}, {Text: title, Style: styleBold}}
	if label := activityStatusLabel(activity); activity.Completed && label != "" {
		header = append(header, styledSpan{Text: " · " + label, Style: styleFailure})
	}
	lines := []styledLine{header}
	path := viewImagePath(activity)
	if path != "" {
		lines = append(lines, styledLine{{Text: "  ├ ", Style: styleDim}, {Text: path, Style: styleAccent}})
	}
	if summary := viewImageSummary(activity); summary != "" {
		prefix := "  └ "
		if path == "" {
			prefix = "  └ "
		}
		lines = append(lines, styledLine{{Text: prefix, Style: styleDim}, {Text: summary, Style: styleDim}})
	} else if activity.Completed && strings.TrimSpace(activity.Result) != "" {
		lines = append(lines, styledLine{{Text: "  └ ", Style: styleDim}, {Text: summarizeActivityText(activity.Result, 320, 2), Style: styleDim}})
	}
	return lines
}

func viewImagePath(activity *toolActivity) string {
	if activity == nil {
		return ""
	}
	if activity.ToolResult != nil {
		if path, ok := activity.ToolResult.Metadata["path"].(string); ok && strings.TrimSpace(path) != "" {
			return strings.TrimSpace(path)
		}
	}
	return firstNonEmpty(activity.Detail, trimPresentationPrefix(activity.Title, "View image"))
}

func viewImageSummary(activity *toolActivity) string {
	if activity == nil || activity.ToolResult == nil {
		return ""
	}
	metadata := activity.ToolResult.Metadata
	sourceWidth, sourceWidthOK := tuiMetadataInt(metadata, "source_width")
	sourceHeight, sourceHeightOK := tuiMetadataInt(metadata, "source_height")
	preparedWidth, preparedWidthOK := tuiMetadataInt(metadata, "prepared_width")
	preparedHeight, preparedHeightOK := tuiMetadataInt(metadata, "prepared_height")
	mediaType, _ := metadata["prepared_media_type"].(string)
	detail := fmt.Sprint(metadata["detail"])
	parts := make([]string, 0, 3)
	if sourceWidthOK && sourceHeightOK && preparedWidthOK && preparedHeightOK {
		parts = append(parts, fmt.Sprintf("%dx%d → %dx%d", sourceWidth, sourceHeight, preparedWidth, preparedHeight))
	}
	if strings.TrimSpace(mediaType) != "" {
		parts = append(parts, mediaType)
	}
	if detail != "" && detail != "<nil>" {
		parts = append(parts, "detail="+detail)
	}
	return strings.Join(parts, " · ")
}

func tuiMetadataInt(metadata map[string]any, key string) (int, bool) {
	switch value := metadata[key].(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), value == float64(int(value))
	default:
		return 0, false
	}
}
