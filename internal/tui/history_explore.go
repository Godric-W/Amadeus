package tui

import "strings"

func renderExploreLines(activities []*toolActivity, ctx HistoryRenderContext) []styledLine {
	complete, success, partial := true, true, false
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
	} else if complete {
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
