package tui

func renderWebSearchLines(activity *toolActivity, ctx HistoryRenderContext) []styledLine {
	return renderWebActivityLines(activity, ctx, "Searching the web", "Searched the web")
}

func renderWebFetchLines(activity *toolActivity, ctx HistoryRenderContext) []styledLine {
	return renderWebActivityLines(activity, ctx, "Fetching web content", "Fetched web content")
}

func renderWebActivityLines(activity *toolActivity, ctx HistoryRenderContext, runningTitle, completedTitle string) []styledLine {
	complete, success := activity.Completed, activity.Success
	title := runningTitle
	if complete {
		title = completedTitle
	}
	lines := []styledLine{{markerSpan(complete, success, ctx, activityMarkerStyle(activity)), {Text: " "}, {Text: title, Style: styleBold}}}
	if label := activityStatusLabel(activity); complete && label != "" {
		style := styleFailure
		if label == "partial" {
			style = styleAccent
		}
		lines[0] = append(lines[0], styledSpan{Text: " · " + label, Style: style})
	}
	if detail := firstNonEmpty(activity.Detail, activity.Title); detail != "" {
		lines = append(lines, styledLine{{Text: "  └ ", Style: styleDim}, {Text: detail, Style: styleAccent}})
	}
	return lines
}
