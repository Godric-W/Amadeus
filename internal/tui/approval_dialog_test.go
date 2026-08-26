package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/filechange"
	"github.com/Godric-W/Amadeus/internal/policy"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestApprovalDialogBoundsDiffViewportAndSelection(t *testing.T) {
	dialog := &approvalDialog{choices: []approvalChoice{{label: "once"}, {label: "session"}, {label: "deny"}}}
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

func TestApprovalDialogRendersClaudeStyleCommandPrompt(t *testing.T) {
	_, model := newTestModel(t, nil)
	request, err := policy.NewApprovalRequestForPurpose(
		"approval-1",
		"execute_command",
		json.RawMessage(`{"command":"go test ./...","description":"Run the project test suite"}`),
		policy.ApprovalPurposeCommand,
		policy.CommandRiskModerate,
		policy.ApprovalCause{Kind: policy.ApprovalCauseCommand, Code: "host_command"},
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Command = "go test ./..."
	request.Presentation = policy.CommandApprovalPresentation(request.Command, "Run the project test suite", "/workspace/amadeus")
	model.approval = &approvalState{requestID: request.ID, request: request}
	model.approvalDialog = newApprovalDialog(request)

	rendered := xansi.Strip(model.renderApprovalDialog(72))
	lines := strings.Split(rendered, "\n")
	if len(lines) == 0 || strings.Trim(lines[0], "─") != "" || len([]rune(lines[0])) != 72 {
		t.Fatalf("approval separator = %q", lines[0])
	}
	ordered := []string{
		"Bash command",
		"go test ./...",
		"Run the project test suite",
		"This command requires approval",
		"Do you want to proceed?",
		"› 1. Yes",
		"2. Yes, and don't ask again for this exact command during this session",
		"3. No",
		"Esc to reject",
	}
	position := -1
	for _, fragment := range ordered {
		next := strings.Index(rendered[position+1:], fragment)
		if next < 0 {
			t.Fatalf("approval prompt omitted %q:\n%s", fragment, rendered)
		}
		position += next + 1
	}
	for _, forbidden := range []string{"╭", "╮", "╰", "╯", "Command: go test ./...", "Working directory:"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("approval prompt unexpectedly contains %q:\n%s", forbidden, rendered)
		}
	}
	if count := strings.Count(rendered, "›"); count != 1 {
		t.Fatalf("approval prompt selection pointer count = %d:\n%s", count, rendered)
	}
}
