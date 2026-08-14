package tui

import (
	"testing"

	"github.com/Godric-W/Amadeus/internal/filechange"
	"github.com/Godric-W/Amadeus/internal/policy"
)

func TestApprovalDialogBoundsDiffViewportAndSelection(t *testing.T) {
	dialog := &approvalDialog{choices: []fullscreenApprovalChoice{{label: "once"}, {label: "session"}, {label: "deny"}}}
	dialog.move(-1)
	if dialog.selected != 2 {
		t.Fatalf("selection did not wrap backward: %d", dialog.selected)
	}
	dialog.move(1)
	if dialog.selected != 0 {
		t.Fatalf("selection did not wrap forward: %d", dialog.selected)
	}
	dialog.scroll(100, 3, 10)
	if dialog.diffTop != 7 {
		t.Fatalf("diff viewport exceeded lower bound: %d", dialog.diffTop)
	}
	dialog.scroll(-100, 3, 10)
	if dialog.diffTop != 0 {
		t.Fatalf("diff viewport exceeded upper bound: %d", dialog.diffTop)
	}
}

func TestApprovalDiffLinesPreserveStructuredHunks(t *testing.T) {
	request := policy.ApprovalRequest{Diff: &filechange.Preview{Hunks: []filechange.DiffHunk{
		{Header: "@@ first", Lines: []string{"@@ first", "-old", "+new"}},
		{Header: "@@ second", Lines: []string{"@@ second", " context"}},
	}}}
	lines := approvalDiffLines(request)
	want := []string{"@@ first", "-old", "+new", "@@ second", " context"}
	if len(lines) != len(want) {
		t.Fatalf("unexpected diff lines: %#v", lines)
	}
	for index := range want {
		if lines[index] != want[index] {
			t.Fatalf("diff line %d = %q, want %q", index, lines[index], want[index])
		}
	}
}
