package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/filechange"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
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
	questionLine := -1
	for index, line := range lines {
		if strings.Contains(line, "Do you want to proceed?") {
			questionLine = index
			break
		}
	}
	if questionLine < 0 || questionLine+1 >= len(lines) || !strings.Contains(lines[questionLine+1], "› 1. Yes") {
		t.Fatalf("approval question and options are not adjacent:\n%s", rendered)
	}
}

func TestApprovalQuestionUsesPlainBodyColor(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })
	_, model := newTestModel(t, nil)
	model.palette = terminalPalette{Level: colorLevelTrueColor, Dark: true, Foreground: terminalRGB{225, 225, 225}, Background: terminalRGB{18, 18, 18}}
	request, err := policy.NewApprovalRequestForPurpose(
		"approval-plain", "execute_command", json.RawMessage(`{"command":"go test ./..."}`),
		policy.ApprovalPurposeCommand, policy.CommandRiskModerate,
		policy.ApprovalCause{Kind: policy.ApprovalCauseCommand, Code: "host_command"},
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Command = "go test ./..."
	request.Presentation = policy.CommandApprovalPresentation(request.Command, "Run tests", "/workspace")
	model.approval = &approvalState{requestID: request.ID, request: request}
	model.approvalDialog = newApprovalDialog(request)
	for _, line := range strings.Split(model.renderApprovalDialog(72), "\n") {
		if strings.Contains(line, "Do you want to proceed?") {
			if strings.Contains(line, "\x1b[") {
				t.Fatalf("approval question is not plain body text: %q", line)
			}
			return
		}
	}
	t.Fatal("approval question was not rendered")
}
