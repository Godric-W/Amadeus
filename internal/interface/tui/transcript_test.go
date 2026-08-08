package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestTranscriptLayoutOwnsCellSpacing(t *testing.T) {
	ctx := noColorRenderContext()
	cells := []transcriptCell{newTextCell(cellUser, "你好"), newTextCell(cellAssistant, "回答"), finalMessageSeparatorCell{}, newTextCell(cellUser, "继续")}
	rendered := xansi.Strip(renderTranscriptCells(cells, ctx))
	if strings.Contains(rendered, "\n\n\n") {
		t.Fatalf("layout introduced multiple blank lines: %q", rendered)
	}
	if strings.Count(rendered, "\n\n") != 3 {
		t.Fatalf("cell spacing = %q", rendered)
	}
	for _, cell := range cells {
		for _, line := range cell.RawLines() {
			if strings.HasPrefix(line, "\n") || strings.HasSuffix(line, "\n") {
				t.Fatalf("cell carries fake newline: %q", line)
			}
		}
	}
}

func TestFinalMessageSeparatorShortAndLongRuns(t *testing.T) {
	ctx := noColorRenderContext()
	short := xansi.Strip((finalMessageSeparatorCell{elapsed: 60 * time.Second}).Render(ctx))
	long := xansi.Strip((finalMessageSeparatorCell{elapsed: 61 * time.Second}).Render(ctx))
	if strings.Contains(short, "Worked for") || !strings.Contains(short, "────") {
		t.Fatalf("short separator = %q", short)
	}
	if !strings.Contains(long, "Worked for 1m 01s") {
		t.Fatalf("long separator = %q", long)
	}
}

func TestTranscriptStateFlushesActiveCellOnce(t *testing.T) {
	state := transcriptState{ActiveCell: newToolHistoryCell()}
	state.ActiveCell.Apply(toolStarted("call"))
	state.ActiveCell.Apply(toolCompleted("call"))
	state.flushActive()
	state.flushActive()
	if len(state.Cells) != 1 || state.ActiveCell != nil {
		t.Fatalf("flush state = %#v", state)
	}
}

func toolStarted(callID string) event.ToolCallStarted {
	return event.ToolCallStarted{CallID: callID, ToolName: "read_file", SideEffect: "read", ActionSummary: "Read file"}
}

func toolCompleted(callID string) event.ToolCallCompleted {
	return event.ToolCallCompleted{CallID: callID, ToolName: "read_file", Success: true}
}
