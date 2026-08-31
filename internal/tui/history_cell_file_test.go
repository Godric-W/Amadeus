package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/filechange"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestToolHistoryRoutesCanonicalToolNames(t *testing.T) {
	cell := newToolHistoryCell()
	read := toolStartedMessage("read", "read", "read", "Read internal/a.go", "")
	grep := toolStartedMessage("grep", "grep", "read", "Search Approval in internal", "")
	glob := toolStartedMessage("glob", "glob", "read", "Find **/*.go", "")
	for _, started := range []protocol.ItemStartedEvent{read, grep, glob} {
		cell.Apply(started)
		cell.Apply(toolCompletedMessage(started, protocol.ItemStatusCompleted, "", "0s", false))
	}

	rendered := xansi.Strip(renderHistoryCellForTest(cell, noColorRenderContext()))
	for _, expected := range []string{"Read internal/a.go", "Grep Approval in internal", "Glob **/*.go"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("rendered exploration omitted %q: %q", expected, rendered)
		}
	}
	if strings.Contains(rendered, "Search Approval") || strings.Contains(rendered, "Find **/*.go") {
		t.Fatalf("rendered exploration leaked presentation verbs: %q", rendered)
	}
}

func TestFileChangeCellRendersStructuredPreview(t *testing.T) {
	cell := newToolHistoryCell()
	started := toolStartedMessage("write", "write", "write", "Create internal/new.go", "")
	cell.Apply(started)
	completed := started.Item
	completed.Status = protocol.ItemStatusCompleted
	completed.CompletedAt = time.Now().UTC()
	completed.ToolResult = &tool.ToolResult{
		ToolName: "write",
		Display: tool.ToolDisplayResult{
			Kind:  tool.ToolDisplayFileChange,
			Title: "internal/new.go",
			Data: filechange.Preview{
				Path:      "internal/new.go",
				Operation: filechange.OperationCreate,
				Stats:     filechange.DiffStats{Insertions: 8, Deletions: 0},
			},
		},
	}
	cell.Apply(protocol.ItemCompletedEvent{Item: completed})

	rendered := xansi.Strip(renderHistoryCellForTest(cell, noColorRenderContext()))
	if !strings.Contains(rendered, "Created internal/new.go") || !strings.Contains(rendered, "+8 -0 lines") {
		t.Fatalf("structured file change projection = %q", rendered)
	}
	if strings.Contains(rendered, "Ran") {
		t.Fatalf("file change was rendered as command execution: %q", rendered)
	}
}

func TestFileChangeCellShowsApprovalWaiting(t *testing.T) {
	cell := newToolHistoryCell()
	started := toolStartedMessage("edit", "edit", "write", "Update internal/config.go", "")
	cell.Apply(started)
	if !cell.SetApprovalState("edit", true) {
		t.Fatal("approval state did not change")
	}
	rendered := xansi.Strip(renderHistoryCellForTest(cell, noColorRenderContext()))
	if !strings.Contains(rendered, "Waiting for approval") || !strings.Contains(rendered, "internal/config.go") {
		t.Fatalf("approval waiting projection = %q", rendered)
	}
}

func TestFileChangeCellShowsDeniedCompletion(t *testing.T) {
	cell := newToolHistoryCell()
	started := toolStartedMessage("write-denied", "write", "write", "Create denied.txt", "")
	cell.Apply(started)
	cell.Apply(toolCompletedMessage(started, protocol.ItemDeclined, "user denied the request", "0s", false))
	rendered := xansi.Strip(renderHistoryCellForTest(cell, noColorRenderContext()))
	if !strings.Contains(rendered, "denied") || strings.Contains(rendered, "Ran") {
		t.Fatalf("denied file change projection = %q", rendered)
	}
}

func TestRestoreCompletedToolItemsPreservesToolSpecificProjection(t *testing.T) {
	readStarted := toolStartedMessage("read-replay", "read", "read", "Read docs/design.md", "")
	readCompleted := toolCompletedMessage(readStarted, protocol.ItemStatusCompleted, "content", "1ms", false).Item
	readCompleted.Payload = protocol.ToolCallItemPayload{DurationMS: 1, Partial: false, ActionSummary: "Read docs/design.md", SideEffect: "read"}
	writeStarted := toolStartedMessage("write-replay", "write", "write", "Create replay.txt", "")
	writeCompleted := toolCompletedMessage(writeStarted, protocol.ItemStatusCompleted, "updated replay.txt", "2ms", false).Item
	writeCompleted.Payload = protocol.FileChangeItemPayload{DurationMS: 2, Partial: false, ActionSummary: "Create replay.txt", SideEffect: "write"}
	writeCompleted.ToolResult = &tool.ToolResult{
		ToolName: "write",
		Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayFileChange, Data: filechange.Preview{
			Path: "replay.txt", Operation: filechange.OperationCreate, AfterHash: "after",
			Stats: filechange.DiffStats{Insertions: 2}, UnifiedDiff: "+one\n+two",
		}},
	}

	_, replay := newTestModel(t, func(options *ApplicationOptions) {
		options.Snapshot.SessionID = protocol.SessionIDFromThreadID(testThreadID(3))
		options.Snapshot.ThreadID = testThreadID(3)
		options.Snapshot.Items = []protocol.TurnItem{readCompleted, writeCompleted}
	})
	if len(replay.historyCells) != 2 || replay.transcript.ActiveCell != nil {
		t.Fatalf("replay history state = cells:%d active:%T", len(replay.historyCells), replay.transcript.ActiveCell)
	}
	rendered := xansi.Strip(renderHistoryCells(replay.historyCells, HistoryRenderRich, replay.historyRenderContext()))
	for _, expected := range []string{"Explored", "Read docs/design.md", "Created replay.txt", "+2 -0 lines"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("replay projection omitted %q: %q", expected, rendered)
		}
	}
}
