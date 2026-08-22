package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const commandListMaxVisible = 8

type listVisualItem struct {
	Name           string
	Description    string
	Selected       bool
	Disabled       bool
	DisabledReason string
}

type listVisual struct {
	Title               string
	Subtitle            string
	Hint                string
	InputLabel          string
	InputValue          string
	Details             []string
	Items               []listVisualItem
	EmptyText           string
	HideSelectionMarker bool
}

func (model fullscreenModel) renderListVisual(visual listVisual, width int) string {
	width = maxInt(20, width)
	lines := make([]string, 0, len(visual.Items)+4)
	if strings.TrimSpace(visual.Title) != "" {
		header := model.palette.strong().Render(strings.TrimSpace(visual.Title))
		if hint := strings.TrimSpace(visual.Hint); hint != "" {
			header += "  " + model.palette.dim().Render(hint)
		}
		lines = append(lines, truncateFullscreen(header, width))
	}
	if subtitle := strings.TrimSpace(visual.Subtitle); subtitle != "" {
		lines = append(lines, model.palette.dim().Render(truncateFullscreen(subtitle, width)))
	}
	for _, detail := range visual.Details {
		if detail = strings.TrimSpace(detail); detail != "" {
			lines = append(lines, model.palette.plain().Render(truncateFullscreen(detail, width)))
		}
	}
	if visual.InputLabel != "" {
		prefix := model.palette.accent().Render(visual.InputLabel)
		valueWidth := maxInt(1, width-lipgloss.Width(visual.InputLabel))
		lines = append(lines, prefix+model.palette.plain().Render(truncateFullscreen(visual.InputValue, valueWidth)))
	}
	if len(visual.Items) == 0 && strings.TrimSpace(visual.EmptyText) != "" {
		lines = append(lines, model.palette.dim().Render(truncateFullscreen(visual.EmptyText, width)))
	}
	nameWidth := 0
	for _, item := range visual.Items {
		nameWidth = maxInt(nameWidth, lipgloss.Width(item.Name))
	}
	nameWidth = minInt(nameWidth, maxInt(8, width/2))
	for _, item := range visual.Items {
		lines = append(lines, model.renderListVisualItem(item, nameWidth, width, visual.HideSelectionMarker))
	}
	return strings.Join(lines, "\n")
}

func (model fullscreenModel) renderListVisualItem(item listVisualItem, nameWidth, width int, hideSelectionMarker bool) string {
	prefix := "  "
	if item.Selected && !hideSelectionMarker {
		prefix = model.palette.selection().Render("›") + " "
	}
	name := truncateFullscreen(strings.TrimSpace(item.Name), nameWidth)
	padding := strings.Repeat(" ", maxInt(0, nameWidth-lipgloss.Width(name)))
	description := strings.TrimSpace(item.Description)
	if item.Disabled {
		reason := strings.TrimSpace(item.DisabledReason)
		if reason == "" {
			reason = "disabled"
		}
		if description != "" {
			description += " · "
		}
		description += reason
	}
	nameStyle := model.palette.plain()
	descriptionStyle := model.palette.dim()
	if item.Selected && !item.Disabled {
		nameStyle = model.palette.selection()
		descriptionStyle = model.palette.selection()
	} else if item.Disabled {
		nameStyle = model.palette.dim()
		if item.Selected && !hideSelectionMarker {
			prefix = model.palette.dim().Render("›") + " "
		}
	}
	line := prefix + nameStyle.Render(name) + padding
	if description != "" {
		line += "  " + descriptionStyle.Render(description)
	}
	return truncateFullscreen(line, width)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
