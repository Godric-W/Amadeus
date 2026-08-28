package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestHistoryLayoutOwnsCellSpacing(t *testing.T) {
	ctx := noColorRenderContext()
	cells := []HistoryCell{
		NewUserMessageCell("你好"),
		NewAgentMarkdownCell(newMarkdownSource("回答", "")),
		FinalMessageSeparator{},
		NewUserMessageCell("继续"),
	}
	rendered := xansi.Strip(renderHistoryCells(cells, HistoryRenderRich, ctx))
	lines := strings.Split(rendered, "\n")
	indices := map[string]int{"user": -1, "agent": -1, "separator": -1, "next": -1}
	for index, line := range lines {
		switch {
		case strings.Contains(line, "你好"):
			indices["user"] = index
		case strings.Contains(line, "回答"):
			indices["agent"] = index
		case strings.HasPrefix(line, "──"):
			indices["separator"] = index
		case strings.Contains(line, "继续"):
			indices["next"] = index
		}
	}
	if indices["agent"] != indices["user"]+3 || indices["separator"] != indices["agent"]+2 || indices["next"] != indices["separator"]+2 {
		t.Fatalf("cell spacing indices=%v rendered=%q", indices, rendered)
	}
	for _, cell := range cells {
		for _, line := range cell.RawLines() {
			if strings.HasPrefix(line, "\n") || strings.HasSuffix(line, "\n") {
				t.Fatalf("cell carries fake newline: %q", line)
			}
		}
	}
}

func TestFinalMessageSeparatorOverridesDefaultTwoBlankRows(t *testing.T) {
	if got := historyLeadingBlankRows(NewAgentMarkdownCell(newMarkdownSource("answer", ""))); got != 2 {
		t.Fatalf("agent leading blank rows = %d", got)
	}
	if got := historyLeadingBlankRows(FinalMessageSeparator{}); got != 1 {
		t.Fatalf("separator leading blank rows = %d", got)
	}
	if got := historyLeadingBlankRows(AgentMessageCell{First: false}); got != 0 {
		t.Fatalf("stream continuation leading blank rows = %d", got)
	}
	agent := NewAgentMarkdownCell(newMarkdownSource("answer", ""))
	separator := FinalMessageSeparator{}
	toolCell := newToolHistoryCell()
	for name, test := range map[string]struct {
		previous HistoryCell
		current  HistoryCell
		want     int
	}{
		"separator to agent": {previous: separator, current: agent, want: 1},
		"agent to separator": {previous: agent, current: separator, want: 1},
		"agent to tool":      {previous: agent, current: toolCell, want: 1},
		"tool to agent":      {previous: toolCell, current: agent, want: 1},
		"tool to tool":       {previous: toolCell, current: toolCell, want: 1},
	} {
		if got := historyBoundaryBlankRows(test.previous, test.current); got != test.want {
			t.Fatalf("%s blank rows = %d, want %d", name, got, test.want)
		}
	}
}

func TestHistoryRenderModeSeparatesRichAndRaw(t *testing.T) {
	ctx := noColorRenderContext()
	cell := NewAgentMarkdownCell(newMarkdownSource("**strong**", ""))
	rich := xansi.Strip(renderHistoryCells([]HistoryCell{cell}, HistoryRenderRich, ctx))
	raw := xansi.Strip(renderHistoryCells([]HistoryCell{cell}, HistoryRenderRaw, ctx))
	if rich != "• strong" {
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
		NewAgentMarkdownCell(newMarkdownSource("**answer**", "")),
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
	model := appModel{transcript: TranscriptState{ActiveCell: newToolHistoryCell()}}
	model.transcript.ActiveCell.Apply(toolStarted("call"))
	model.transcript.ActiveCell.Apply(toolCompleted("call"))
	model.flushActiveHistoryCell()
	model.flushActiveHistoryCell()
	if len(model.historyCells) != 1 || model.transcript.ActiveCell != nil {
		t.Fatalf("flush state = %#v", model.transcript)
	}
}

func TestTranscriptStateBumpsActiveCellRevision(t *testing.T) {
	model := appModel{transcript: TranscriptState{ActiveCell: newToolHistoryCell()}}
	model.applyEvent(testProtocolEvent(testThreadID(1), "turn-1", toolStarted("call")))
	if model.transcript.ActiveCellRevision == 0 {
		t.Fatal("active cell revision did not change after mutation")
	}
	before := model.transcript.ActiveCellRevision
	model.applyEvent(testProtocolEvent(testThreadID(1), "turn-1", toolCompleted("call")))
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
	if rendered != "first\nsecond\n\n\nthird" {
		t.Fatalf("continuation spacing = %q", rendered)
	}
}

func TestHistoryCellArchitectureHasNoLegacyMainChain(t *testing.T) {
	for _, path := range []string{
		"history_cell.go",
		"history_cell_tools.go",
		"application.go",
		"application_update.go",
		"application_events.go",
		"application_view.go",
	} {
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
			"func (model *appModel) flushTranscript",
			"output = \"\\n\" + output",
		} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s contains legacy history chain %q", path, forbidden)
			}
		}
	}
}

func toolStarted(callID string) protocol.ItemStartedEvent {
	return toolStartedMessage(callID, "read", "read", "Read file", "")
}

func toolCompleted(callID string) protocol.ItemCompletedEvent {
	return toolCompletedMessage(toolStarted(callID), protocol.ItemStatusCompleted, "", "0s", false)
}
