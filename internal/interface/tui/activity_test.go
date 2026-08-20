package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	xansi "github.com/charmbracelet/x/ansi"
)

func noColorRenderContext() HistoryRenderContext {
	now := time.Unix(100, 0)
	return HistoryRenderContext{Width: 80, Palette: terminalPalette{Level: colorLevelNone, NoColor: true, Dark: true}, Now: now, MotionStart: now, Motion: motionReduced}
}

func renderHistoryCellForTest(cell HistoryCell, ctx HistoryRenderContext) string {
	return renderStyledLines(historyLinesForMode(cell, HistoryRenderRich, ctx), ctx)
}

func toolStartedMessage(callID, toolName, sideEffect, summary, detail string) protocol.ItemStartedEvent {
	now := time.Now().UTC()
	return protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: protocol.ItemID(callID), CallID: callID, ToolName: toolName, Kind: protocol.ItemToolCall, Status: protocol.ItemInProgress, CreatedAt: now, Payload: map[string]any{"side_effect": sideEffect, "action_summary": summary, "detail": detail}}}
}

func toolCompletedMessage(started protocol.ItemStartedEvent, status protocol.ItemStatus, text, duration string, partial bool) protocol.ItemCompletedEvent {
	now := time.Now().UTC()
	item := started.Item
	item.Status = status
	item.CompletedAt = now
	item.Text = text
	payload := map[string]any{"duration": duration, "partial": partial}
	if startedPayload, ok := started.Item.Payload.(map[string]any); ok {
		for key, value := range startedPayload {
			payload[key] = value
		}
	}
	item.Payload = payload
	return protocol.ItemCompletedEvent{Item: item}
}

func TestToolHistoryCellGroupsExplorationAndDeduplicatesReads(t *testing.T) {
	cell := newToolHistoryCell()
	for _, started := range []protocol.ItemStartedEvent{
		toolStartedMessage("read-1", "read", "read", "Read docs/design.md", ""),
		toolStartedMessage("read-2", "read", "read", "Read docs/design.md", ""),
		toolStartedMessage("search-1", "grep", "read", "Search M9V", ""),
	} {
		cell.Apply(started)
		cell.Apply(toolCompletedMessage(started, protocol.ItemStatusCompleted, "", "0s", false))
	}
	rendered := xansi.Strip(renderHistoryCellForTest(cell, noColorRenderContext()))
	if !strings.Contains(rendered, "Explored") || strings.Count(rendered, "docs/design.md") != 1 || !strings.Contains(rendered, "Grep M9V") {
		t.Fatalf("unexpected explore projection: %q", rendered)
	}
	if strings.Count(rendered, "└") != 1 || !strings.Contains(rendered, "\n    Grep M9V") {
		t.Fatalf("explore tree should use one branch marker: %q", rendered)
	}
}

func TestToolHistoryCellExecLifecycleAndOutputBounds(t *testing.T) {
	cell := newToolHistoryCell()
	started := toolStartedMessage("exec-1", "execute_command", "write", "Run tests", "go test ./...")
	cell.Apply(started)
	active := xansi.Strip(renderHistoryCellForTest(cell, noColorRenderContext()))
	if !strings.Contains(active, "Running") || !strings.Contains(active, "go test ./...") || cell.IsComplete() {
		t.Fatalf("active exec projection: %q", active)
	}
	cell.Apply(toolCompletedMessage(started, protocol.ItemStatusCompleted, "1\n2\n3\n4\n5\n6\n7", "1.25s", false))
	rendered := xansi.Strip(renderHistoryCellForTest(cell, noColorRenderContext()))
	for _, expected := range []string{"Ran", "1", "5", "… +2 lines"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("exec projection omitted %q: %q", expected, rendered)
		}
	}
	if strings.Contains(rendered, "\n  │ 6") || !cell.IsComplete() {
		t.Fatalf("exec output was not bounded: %q", rendered)
	}
	if strings.Count(rendered, "└") != 1 || strings.Contains(rendered, "✓") {
		t.Fatalf("exec preview should use one output branch and no transcript-only checkmark: %q", rendered)
	}
}

func TestToolHistoryCellPreservesCallSequenceAndFailure(t *testing.T) {
	cell := newToolHistoryCell()
	second := toolStartedMessage("second", "execute_command", "write", "Second", "")
	first := toolStartedMessage("first", "web_search", "network", "First", "")
	cell.Apply(second)
	cell.Apply(first)
	cell.Apply(toolCompletedMessage(first, protocol.ItemStatusCompleted, "", "0s", false))
	cell.Apply(toolCompletedMessage(second, protocol.ItemFailed, "exit status 1", "0s", false))
	rendered := xansi.Strip(renderHistoryCellForTest(cell, noColorRenderContext()))
	if strings.Index(rendered, "Second") > strings.Index(rendered, "First") || (!strings.Contains(rendered, "failed") && !strings.Contains(rendered, "exit status 1")) {
		t.Fatalf("sequence/failure projection: %q", rendered)
	}
}

func TestExecCellPreservesYouRanSemantic(t *testing.T) {
	cell := newToolHistoryCell()
	started := toolStartedMessage("user-exec", "execute_command", "write", "You ran git status", "git status")
	cell.Apply(started)
	cell.Apply(toolCompletedMessage(started, protocol.ItemStatusCompleted, "", "0s", false))
	if rendered := xansi.Strip(renderHistoryCellForTest(cell, noColorRenderContext())); !strings.Contains(rendered, "You ran") {
		t.Fatalf("user command semantic missing: %q", rendered)
	}
}
