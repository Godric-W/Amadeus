package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	xansi "github.com/charmbracelet/x/ansi"
)

func noColorRenderContext() HistoryRenderContext {
	now := time.Unix(100, 0)
	return HistoryRenderContext{
		Width: 80, Palette: terminalPalette{Level: colorLevelNone, NoColor: true, Dark: true},
		Now: now, MotionStart: now, Motion: motionReduced,
	}
}

func renderHistoryCellForTest(cell HistoryCell, ctx HistoryRenderContext) string {
	return renderStyledLines(historyLinesForMode(cell, HistoryRenderRich, ctx), ctx)
}

func TestToolHistoryCellGroupsExplorationAndDeduplicatesReads(t *testing.T) {
	cell := newToolHistoryCell()
	for _, started := range []event.ToolCallStarted{
		{CallID: "read-1", ToolName: "read", SideEffect: "read", ActionSummary: "Read docs/design.md"},
		{CallID: "read-2", ToolName: "read", SideEffect: "read", ActionSummary: "Read docs/design.md"},
		{CallID: "search-1", ToolName: "grep", SideEffect: "read", ActionSummary: "Search M9V"},
	} {
		cell.Apply(started)
		cell.Apply(event.ToolCallCompleted{CallID: started.CallID, ToolName: started.ToolName, Success: true})
	}
	rendered := xansi.Strip(renderHistoryCellForTest(cell, noColorRenderContext()))
	if !strings.Contains(rendered, "Explored") || strings.Count(rendered, "docs/design.md") != 1 || !strings.Contains(rendered, "Search M9V") {
		t.Fatalf("unexpected explore projection: %q", rendered)
	}
	if strings.Count(rendered, "└") != 1 || !strings.Contains(rendered, "\n    Search M9V") {
		t.Fatalf("explore tree should use one branch marker: %q", rendered)
	}
}

func TestToolHistoryCellExecLifecycleAndOutputBounds(t *testing.T) {
	cell := newToolHistoryCell()
	cell.Apply(event.ToolCallStarted{CallID: "exec-1", ToolName: "execute_command", SideEffect: "write", ActionSummary: "Run tests", Detail: "go test ./..."})
	active := xansi.Strip(renderHistoryCellForTest(cell, noColorRenderContext()))
	if !strings.Contains(active, "Running") || !strings.Contains(active, "go test ./...") || cell.IsComplete() {
		t.Fatalf("active exec projection: %q", active)
	}
	cell.Apply(event.ToolCallCompleted{CallID: "exec-1", ToolName: "execute_command", Success: true, Duration: 1250 * time.Millisecond, Summary: "1\n2\n3\n4\n5\n6\n7"})
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
	cell.Apply(event.ToolCallStarted{CallID: "second", ToolName: "execute_command", SideEffect: "write", ActionSummary: "Second"})
	cell.Apply(event.ToolCallStarted{CallID: "first", ToolName: "web_search", SideEffect: "network", ActionSummary: "First"})
	cell.Apply(event.ToolCallCompleted{CallID: "first", ToolName: "web_search", Success: true})
	cell.Apply(event.ToolCallCompleted{CallID: "second", ToolName: "execute_command", Success: false, Summary: "exit status 1"})
	rendered := xansi.Strip(renderHistoryCellForTest(cell, noColorRenderContext()))
	if strings.Index(rendered, "Second") > strings.Index(rendered, "First") || !strings.Contains(rendered, "failed") && !strings.Contains(rendered, "exit status 1") {
		t.Fatalf("sequence/failure projection: %q", rendered)
	}
}

func TestExecCellPreservesYouRanSemantic(t *testing.T) {
	cell := newToolHistoryCell()
	cell.Apply(event.ToolCallStarted{CallID: "user-exec", ToolName: "execute_command", SideEffect: "write", ActionSummary: "You ran git status", Detail: "git status"})
	cell.Apply(event.ToolCallCompleted{CallID: "user-exec", ToolName: "execute_command", Success: true})
	if rendered := xansi.Strip(renderHistoryCellForTest(cell, noColorRenderContext())); !strings.Contains(rendered, "You ran") {
		t.Fatalf("user command semantic missing: %q", rendered)
	}
}

func TestActivityKindUsesSafeSideEffectFallback(t *testing.T) {
	if got := activityKindFromSideEffect("network"); got != activityNetwork {
		t.Fatalf("network kind = %q", got)
	}
	if got := activityKindFromSideEffect("write"); got != activityRun {
		t.Fatalf("write kind = %q", got)
	}
	if got := activityKindFromSideEffect("unknown"); got != activityRun {
		t.Fatalf("unknown kind = %q", got)
	}
}
