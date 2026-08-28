package tui

import "strings"

type transcriptViewportState struct {
	top          int
	followBottom bool
	initialized  bool
	anchor       string
}

func (state transcriptViewportState) project(rendered string, height int) string {
	if rendered == "" || height == 0 {
		return ""
	}
	if height < 0 {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	if len(lines) <= height {
		return rendered
	}
	maximumTop := len(lines) - height
	top := state.resolveTop(lines, maximumTop)
	if !state.initialized || state.followBottom {
		top = maximumTop
	}
	return strings.Join(lines[top:top+height], "\n")
}

func (state *transcriptViewportState) scroll(rendered string, height, delta int) bool {
	if state == nil || rendered == "" || height <= 0 || delta == 0 {
		return false
	}
	lineCount := len(strings.Split(rendered, "\n"))
	maximumTop := maxInt(0, lineCount-height)
	lines := strings.Split(rendered, "\n")
	current := state.resolveTop(lines, maximumTop)
	if !state.initialized || state.followBottom {
		current = maximumTop
	}
	next := minInt(maxInt(0, current+delta), maximumTop)
	state.initialized = true
	state.top = next
	state.followBottom = next == maximumTop
	if state.followBottom {
		state.anchor = ""
	} else {
		state.anchor = lines[next]
	}
	return next != current
}

func (state transcriptViewportState) resolveTop(lines []string, maximumTop int) int {
	top := minInt(maxInt(0, state.top), maximumTop)
	if state.anchor == "" {
		return top
	}
	for index, line := range lines {
		if line == state.anchor {
			return minInt(index, maximumTop)
		}
	}
	return top
}

func clampViewHeight(rendered string, height int) string {
	if rendered == "" || height <= 0 {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	if len(lines) <= height {
		return rendered
	}
	return strings.Join(lines[len(lines)-height:], "\n")
}
