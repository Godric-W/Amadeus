package tui

import (
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

type HistoryRenderMode uint8

const (
	HistoryRenderRich HistoryRenderMode = iota
	HistoryRenderRaw
)

type semanticStyle uint8

const (
	stylePlain semanticStyle = iota
	styleDim
	styleBold
	styleAccent
	styleCommand
	styleSuccess
	styleFailure
	styleWarning
	styleUser
	styleSeparator
	styleRendered
)

type styledSpan struct {
	Text  string
	Style semanticStyle
}

type styledLine []styledSpan

type HistoryRenderContext struct {
	Width       int
	Palette     terminalPalette
	Markdown    *glamour.TermRenderer
	Now         time.Time
	MotionStart time.Time
	Motion      motionMode
}

type HistoryCell interface {
	DisplayLines(HistoryRenderContext) []styledLine
	RawLines() []string
	IsStreamContinuation() bool
}

type ActiveHistoryCell interface {
	HistoryCell
	Apply(protocol.EventMsg) bool
	Complete() HistoryCell
	IsComplete() bool
}

type TranscriptState struct {
	ActiveCell                 ActiveHistoryCell
	ActiveCellRevision         uint64
	NeedsFinalMessageSeparator bool
	HadWorkActivity            bool
	LastAgentMarkdown          string
}

func (state *TranscriptState) bumpActiveCellRevision() { state.ActiveCellRevision++ }
func (state *TranscriptState) reset()                  { *state = TranscriptState{} }

type FinalMessageSeparator struct{ Elapsed time.Duration }

func (cell FinalMessageSeparator) DisplayLines(ctx HistoryRenderContext) []styledLine {
	width := maxInt(12, ctx.Width)
	label := ""
	if cell.Elapsed > time.Minute {
		label = "─ Worked for " + formatElapsedCompact(cell.Elapsed) + " ─"
	}
	if label == "" {
		return []styledLine{{{Text: strings.Repeat("─", width), Style: styleSeparator}}}
	}
	line := xansi.Truncate(label+strings.Repeat("─", maxInt(0, width-lipgloss.Width(label))), width, "")
	return []styledLine{{{Text: line, Style: styleSeparator}}}
}

func (cell FinalMessageSeparator) RawLines() []string {
	if cell.Elapsed <= time.Minute {
		return nil
	}
	return []string{"Worked for " + formatElapsedCompact(cell.Elapsed)}
}

func (FinalMessageSeparator) IsStreamContinuation() bool { return false }
