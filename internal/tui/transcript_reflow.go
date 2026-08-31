package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const transcriptReflowDebounce = 75 * time.Millisecond

type transcriptReflowMsg struct {
	generation uint64
}

// transcriptReflowState tracks the terminal size for the native history sink.
// HistoryCell remains the source of truth; this state only schedules a
// replacement of terminal-owned wrapped rows after a resize.
type transcriptReflowState struct {
	initialized bool
	width       int
	height      int
	pending     bool
	generation  uint64
}

func (state *transcriptReflowState) noteSize(width, height int, historyPrinted bool) tea.Cmd {
	if state == nil || width <= 0 || height <= 0 {
		return nil
	}
	if !state.initialized {
		state.initialized = true
		state.width, state.height = width, height
		return nil
	}
	changed := state.width != width || state.height != height
	state.width, state.height = width, height
	if !changed || !historyPrinted {
		return nil
	}
	state.generation++
	state.pending = true
	generation := state.generation
	return tea.Tick(transcriptReflowDebounce, func(time.Time) tea.Msg {
		return transcriptReflowMsg{generation: generation}
	})
}

func (state *transcriptReflowState) take(msg transcriptReflowMsg) bool {
	if state == nil || !state.pending || msg.generation != state.generation {
		return false
	}
	state.pending = false
	return true
}

func (state *transcriptReflowState) matches(msg transcriptReflowMsg) bool {
	return state != nil && state.pending && msg.generation == state.generation
}

func (state *transcriptReflowState) reset() {
	if state != nil {
		// Advance the generation so a delayed timer from the previous
		// attachment cannot reflow a newly attached transcript.
		generation := state.generation + 1
		*state = transcriptReflowState{generation: generation}
	}
}
