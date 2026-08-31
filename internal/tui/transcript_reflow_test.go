package tui

import (
	"testing"

	"github.com/Godric-W/Amadeus/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func TestTranscriptReflowStateDebouncesToLatestSize(t *testing.T) {
	var state transcriptReflowState
	if command := state.noteSize(80, 24, true); command != nil {
		t.Fatal("initial size scheduled a reflow")
	}
	first := state.noteSize(72, 24, true)
	second := state.noteSize(64, 20, true)
	if first == nil || second == nil || !state.pending {
		t.Fatalf("resize scheduling = first:%v second:%v state:%#v", first != nil, second != nil, state)
	}
	firstMessage, ok := first().(transcriptReflowMsg)
	if !ok {
		t.Fatalf("first resize message = %T", first())
	}
	if state.take(firstMessage) {
		t.Fatal("stale resize generation was accepted")
	}
	secondMessage, ok := second().(transcriptReflowMsg)
	if !ok {
		t.Fatalf("second resize message = %T", second())
	}
	if !state.take(secondMessage) || state.pending {
		t.Fatalf("latest resize generation was not consumed: %#v", state)
	}
}

func TestTranscriptSurfaceReflowStopsAtTransientStream(t *testing.T) {
	surface := TranscriptSurface{
		sessionHeader: NewSessionHeaderCell("dev", "model", "/workspace"),
		historyCells: []HistoryCell{
			NewUserMessageCell("before stream"),
			AgentMessageCell{ItemID: protocol.ItemID("assistant-1"), First: true},
			NewNoticeHistoryCell("deferred after stream"),
		},
	}
	cells := surface.reflowCells()
	if len(cells) != 2 {
		t.Fatalf("reflow cells = %d, want header plus immutable prefix", len(cells))
	}
	surface.markReflowed()
	if !surface.sessionHeaderPrinted || surface.historyPrintCursor != 1 || !surface.printedVisible {
		t.Fatalf("reflow watermark = %#v", surface)
	}
}

func TestWindowResizeSchedulesSourceBackedHistoryReflow(t *testing.T) {
	_, model := newTestModel(t, nil)
	updated, command := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(appModel)
	if command != nil {
		t.Fatal("first observed terminal size scheduled a reflow")
	}

	updated, command = model.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	model = updated.(appModel)
	if command == nil || !model.transcriptReflow.pending {
		t.Fatalf("resize did not schedule reflow: %#v", model.transcriptReflow)
	}
	message, ok := command().(transcriptReflowMsg)
	if !ok {
		t.Fatalf("resize command message = %T", command())
	}
	updated, reflow := model.Update(message)
	model = updated.(appModel)
	if reflow == nil || model.transcriptReflow.pending || !model.TranscriptSurface.printedVisible {
		t.Fatalf("reflow completion = command:%v state:%#v surface:%#v", reflow != nil, model.transcriptReflow, model.TranscriptSurface)
	}
}
