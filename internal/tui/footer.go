package tui

import (
	"strings"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

const (
	footerLeftPadding    = 2
	footerRightPadding   = 2
	footerColumnGap      = 1
	footerQueueHintFull  = "tab to queue message"
	footerQueueHintShort = "tab to queue"
)

type collaborationModeIndicator uint8

const collaborationModeIndicatorPlan collaborationModeIndicator = 1

type footerState struct {
	StatusLine             statusLineState
	CollaborationIndicator collaborationModeIndicator
}

type footerProps struct {
	Width             int
	Running           bool
	HasQueueableDraft bool
	State             footerState
	Palette           terminalPalette
	LeftPadding       int
	RightPadding      int
}

func collaborationModeIndicatorFor(mode protocol.ModeKind) collaborationModeIndicator {
	if mode == protocol.ModeKindPlan {
		return collaborationModeIndicatorPlan
	}
	return 0
}

func (model appModel) footerView() string {
	return renderFooter(footerProps{
		Width: model.width, Running: model.running, HasQueueableDraft: model.hasQueueableDraft(), State: model.footer,
		Palette: model.palette, LeftPadding: footerLeftPadding,
		RightPadding: footerRightPadding,
	})
}

func (model appModel) composerFooterView() string {
	if model.slashPopup.active() || model.selection != nil || model.approvalDialog != nil || model.userInputDialog != nil {
		return ""
	}
	return model.footerView()
}

func renderFooter(props footerProps) string {
	width := maxInt(0, props.Width)
	leftPadding := minInt(maxInt(0, props.LeftPadding), width)
	rightPadding := minInt(maxInt(0, props.RightPadding), maxInt(0, width-leftPadding))
	contentWidth := maxInt(0, width-leftPadding-rightPadding)
	if contentWidth == 0 {
		return ""
	}
	if props.HasQueueableDraft {
		return renderQueueHintFooter(props, leftPadding, rightPadding, contentWidth)
	}

	segments := append([]statusLineSegment(nil), props.State.StatusLine.Segments...)
	statusLine := renderStatusLineSegments(segments, props.Palette, props.State.StatusLine.ContextUsedPercent)

	indicator := footerIndicatorLabel(props.State.CollaborationIndicator, !props.Running)
	if indicator == "" {
		left := fitFooterLeft(statusLine, contentWidth)
		if left == "" {
			return ""
		}
		return strings.Repeat(" ", leftPadding) + left
	}

	indicator, left := fitFooterColumns(indicator, statusLine, contentWidth, props)
	indicatorWidth := lipgloss.Width(indicator)
	gap := maxInt(0, contentWidth-lipgloss.Width(left)-indicatorWidth)
	return strings.Repeat(" ", leftPadding) + left + strings.Repeat(" ", gap) +
		props.Palette.statusLineStyle(statusAccentMode).Render(indicator) + strings.Repeat(" ", rightPadding)
}

func renderQueueHintFooter(props footerProps, leftPadding, rightPadding, contentWidth int) string {
	mode := footerIndicatorLabel(props.State.CollaborationIndicator, false)
	type candidate struct {
		hint     string
		showMode bool
	}
	candidates := make([]candidate, 0, 4)
	if mode != "" {
		candidates = append(candidates,
			candidate{hint: footerQueueHintFull, showMode: true},
			candidate{hint: footerQueueHintShort, showMode: true},
		)
	}
	candidates = append(candidates,
		candidate{hint: footerQueueHintFull},
		candidate{hint: footerQueueHintShort},
	)
	for _, current := range candidates {
		required := lipgloss.Width(current.hint)
		if current.showMode {
			required += footerColumnGap + lipgloss.Width(mode)
		}
		if required <= contentWidth {
			return renderQueueHintCandidate(props, current.hint, mode, current.showMode, leftPadding, rightPadding, contentWidth)
		}
	}
	hint := xansi.Truncate(footerQueueHintShort, contentWidth, "")
	return strings.Repeat(" ", leftPadding) + props.Palette.dim().Render(hint) +
		strings.Repeat(" ", maxInt(0, contentWidth-lipgloss.Width(hint)+rightPadding))
}

func renderQueueHintCandidate(props footerProps, hint, mode string, showMode bool, leftPadding, rightPadding, contentWidth int) string {
	left := strings.Repeat(" ", leftPadding) + props.Palette.dim().Render(hint)
	if !showMode {
		return left + strings.Repeat(" ", maxInt(0, contentWidth-lipgloss.Width(hint)+rightPadding))
	}
	gap := maxInt(footerColumnGap, contentWidth-lipgloss.Width(hint)-lipgloss.Width(mode))
	return left + strings.Repeat(" ", gap) + props.Palette.statusLineStyle(statusAccentMode).Render(mode) + strings.Repeat(" ", rightPadding)
}

func fitFooterColumns(indicator, statusLine string, contentWidth int, props footerProps) (string, string) {
	compact := footerIndicatorLabel(props.State.CollaborationIndicator, false)
	fullWidth := lipgloss.Width(indicator)
	compactWidth := lipgloss.Width(compact)
	if statusLine == "" {
		if fullWidth <= contentWidth {
			return indicator, ""
		}
		return compact, ""
	}
	fullLeftWidth := maxInt(0, contentWidth-fullWidth-footerColumnGap)
	if fullWidth <= contentWidth && lipgloss.Width(statusLine) <= fullLeftWidth {
		return indicator, statusLine
	}
	compactLeftWidth := maxInt(0, contentWidth-compactWidth-footerColumnGap)
	return compact, fitFooterLeft(statusLine, compactLeftWidth)
}

func footerIndicatorLabel(indicator collaborationModeIndicator, showCycleHint bool) string {
	if indicator != collaborationModeIndicatorPlan {
		return ""
	}
	if showCycleHint {
		return "Plan mode (shift+tab to cycle)"
	}
	return "Plan mode"
}

func fitFooterLeft(source string, width int) string {
	if width <= 0 || source == "" {
		return ""
	}
	if lipgloss.Width(source) > width {
		return xansi.Truncate(source, width, "…")
	}
	return source
}

func renderStatusLineSegments(segments []statusLineSegment, palette terminalPalette, contextUsedPercent int64) string {
	var builder strings.Builder
	separator := palette.dim().Render(" · ")
	for index, segment := range segments {
		if index > 0 {
			builder.WriteString(separator)
		}
		style := palette.statusLineStyle(statusLineAccentForItem(segment.Item))
		if segment.Item == statusLineItemContextUsed {
			style = statusContextStyle(palette, contextUsedPercent)
		}
		builder.WriteString(style.Render(segment.Text))
	}
	return builder.String()
}

func statusContextStyle(palette terminalPalette, percent int64) lipgloss.Style {
	switch {
	case percent >= 90:
		return palette.failure()
	case percent >= 70:
		return palette.warning()
	default:
		return palette.statusLineStyle(statusAccentUsage)
	}
}
