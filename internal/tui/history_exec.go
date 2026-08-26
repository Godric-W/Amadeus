package tui

import "strings"

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
			lines = append(lines, styledLine{{Text: prefix, Style: styleDim}, {Text: output, Style: styleDim}})
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
