package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestHistoryLayoutOwnsCellSpacing(t *testing.T) {
	ctx := noColorRenderContext()
	cells := []HistoryCell{
		NewUserMessageCell("你好"),
		NewAgentMessageCell("回答"),
		FinalMessageSeparator{},
		NewUserMessageCell("继续"),
	}
	rendered := xansi.Strip(renderHistoryCells(cells, HistoryRenderRich, ctx))
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

func TestHistoryRenderModeSeparatesRichAndRaw(t *testing.T) {
	ctx := noColorRenderContext()
	cell := NewAgentMessageCell("**strong**")
	rich := xansi.Strip(renderHistoryCells([]HistoryCell{cell}, HistoryRenderRich, ctx))
	raw := xansi.Strip(renderHistoryCells([]HistoryCell{cell}, HistoryRenderRaw, ctx))
	if rich != "• **strong**" {
		t.Fatalf("rich output = %q", rich)
	}
	if raw != "**strong**" {
		t.Fatalf("raw output = %q", raw)
	}
}

func TestHistoryRawModeNeverCarriesANSI(t *testing.T) {
	ctx := noColorRenderContext()
	ctx.Palette = terminalPalette{Level: colorLevelTrueColor, Dark: true}
	cells := []HistoryCell{
		NewUserMessageCell("hello"),
		NewAgentMessageCell("**answer**"),
		NewErrorHistoryCell("boom"),
	}
	raw := renderHistoryCells(cells, HistoryRenderRaw, ctx)
	if strings.Contains(raw, "\x1b[") {
		t.Fatalf("raw history contains ANSI: %q", raw)
	}
	for _, expected := range []string{"hello", "**answer**", "Error: boom"} {
		if !strings.Contains(raw, expected) {
			t.Fatalf("raw history omitted %q: %q", expected, raw)
		}
	}
}

func TestFinalMessageSeparatorShortAndLongRuns(t *testing.T) {
	ctx := noColorRenderContext()
	short := xansi.Strip(renderHistoryCellForTest(FinalMessageSeparator{Elapsed: 60 * time.Second}, ctx))
	long := xansi.Strip(renderHistoryCellForTest(FinalMessageSeparator{Elapsed: 61 * time.Second}, ctx))
	if strings.Contains(short, "Worked for") || !strings.Contains(short, "────") {
		t.Fatalf("short separator = %q", short)
	}
	if !strings.Contains(long, "Worked for 1m 01s") {
		t.Fatalf("long separator = %q", long)
	}
}

func TestTranscriptStateFlushesActiveHistoryCellOnce(t *testing.T) {
	model := fullscreenModel{transcript: TranscriptState{ActiveCell: newToolHistoryCell()}}
	model.transcript.ActiveCell.Apply(toolStarted("call"))
	model.transcript.ActiveCell.Apply(toolCompleted("call"))
	model.flushActiveHistoryCell()
	model.flushActiveHistoryCell()
	if len(model.historyCells) != 1 || model.transcript.ActiveCell != nil {
		t.Fatalf("flush state = %#v", model.transcript)
	}
}

func TestTranscriptStateBumpsActiveCellRevision(t *testing.T) {
	model := fullscreenModel{transcript: TranscriptState{ActiveCell: newToolHistoryCell()}}
	model.applyEvent(protocol.SessionEvent{ThreadID: "thread-1", TurnID: "turn-1", Message: toolStarted("call")})
	if model.transcript.ActiveCellRevision == 0 {
		t.Fatal("active cell revision did not change after mutation")
	}
	before := model.transcript.ActiveCellRevision
	model.applyEvent(protocol.SessionEvent{ThreadID: "thread-1", TurnID: "turn-1", Message: toolCompleted("call")})
	if model.transcript.ActiveCellRevision <= before {
		t.Fatalf("revision = %d, want > %d", model.transcript.ActiveCellRevision, before)
	}
}

type continuationHistoryCell struct {
	text         string
	continuation bool
}

func (cell continuationHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	return styledLinesFromText(cell.text, stylePlain)
}
func (cell continuationHistoryCell) RawLines() []string         { return []string{cell.text} }
func (cell continuationHistoryCell) IsStreamContinuation() bool { return cell.continuation }

func TestHistoryStreamContinuationDoesNotInsertBlankLine(t *testing.T) {
	ctx := noColorRenderContext()
	cells := []HistoryCell{
		continuationHistoryCell{text: "first"},
		continuationHistoryCell{text: "second", continuation: true},
		continuationHistoryCell{text: "third"},
	}
	rendered := renderHistoryCells(cells, HistoryRenderRaw, ctx)
	if rendered != "first\nsecond\n\nthird" {
		t.Fatalf("continuation spacing = %q", rendered)
	}
}

func TestHistoryCellArchitectureHasNoLegacyMainChain(t *testing.T) {
	for _, path := range []string{"history_cell.go", "history_cell_tools.go", "application.go"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(content)
		for _, forbidden := range []string{
			"type transcriptCell",
			"transcriptCellKind",
			"HistoryCellKind",
			"transcript.Committed",
			"func (model *fullscreenModel) flushTranscript",
			"output = \"\\n\" + output",
		} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s contains legacy history chain %q", path, forbidden)
			}
		}
	}
}

func toolStarted(callID string) protocol.ItemStarted {
	return toolStartedMessage(callID, "read", "read", "Read file", "")
}

func toolCompleted(callID string) protocol.ItemCompleted {
	return toolCompletedMessage(toolStarted(callID), protocol.ItemStatusCompleted, "", "0s", false)
}
