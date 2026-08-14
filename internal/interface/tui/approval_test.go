package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/policy"
)

func TestInlineApprovalPromptChoicesAndRedaction(t *testing.T) {
	for _, test := range []struct {
		input   string
		outcome policy.ApprovalOutcome
		scope   policy.ApprovalScope
	}{
		{input: "y\n", outcome: policy.ApprovalAllow, scope: policy.ApprovalOnce},
		{input: "s\n", outcome: policy.ApprovalAllow, scope: policy.ApprovalSession},
		{input: "n\n", outcome: policy.ApprovalDeny, scope: policy.ApprovalOnce},
	} {
		var output bytes.Buffer
		prompt, err := NewInlineApprovalPrompt(InlineApprovalPromptOptions{
			Input: strings.NewReader(test.input), Output: &output, IsTerminal: func(io.Reader) bool { return true },
		})
		if err != nil {
			t.Fatal(err)
		}
		request := inlineApprovalRequest(t, "Authorization: Bearer secret-value")
		decision, err := prompt.Decide(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if decision.Outcome != test.outcome || decision.Scope != test.scope || decision.Source != policy.ApprovalSourceUser {
			t.Fatalf("unexpected decision: %#v", decision)
		}
		lowerOutput := strings.ToLower(output.String())
		if strings.Contains(output.String(), "secret-value") || strings.Contains(lowerOutput, "unsandbox") || strings.Contains(lowerOutput, "arguments_sha256") || strings.Contains(lowerOutput, "reason:") || !strings.Contains(output.String(), "[y] once / [s] session / [n] no") {
			t.Fatalf("unexpected inline approval transcript: %q", output.String())
		}
	}
}

func TestInlineApprovalPromptDeniesNonTTYAndRetriesInvalidInput(t *testing.T) {
	prompt, err := NewInlineApprovalPrompt(InlineApprovalPromptOptions{
		Input: strings.NewReader("y\n"), Output: &bytes.Buffer{}, IsTerminal: func(io.Reader) bool { return false },
	})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := prompt.Decide(context.Background(), inlineApprovalRequest(t, "writes a file"))
	if err != nil || decision.Allowed() || decision.Source != policy.ApprovalSourcePolicy {
		t.Fatalf("unexpected non-TTY decision: %#v err=%v", decision, err)
	}

	var output bytes.Buffer
	prompt, err = NewInlineApprovalPrompt(InlineApprovalPromptOptions{
		Input: strings.NewReader("always\ny\n"), Output: &output, IsTerminal: func(io.Reader) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	decision, err = prompt.Decide(context.Background(), inlineApprovalRequest(t, "writes a file"))
	if err != nil || !decision.Allowed() || !strings.Contains(output.String(), "Invalid choice. Enter 1, 2, 3, y, s, or n") {
		t.Fatalf("unexpected retry decision: %#v output=%q err=%v", decision, output.String(), err)
	}
}

func TestInlineApprovalPromptPermissionChoiceUsesRunScope(t *testing.T) {
	var output bytes.Buffer
	prompt, err := NewInlineApprovalPrompt(InlineApprovalPromptOptions{
		Input: strings.NewReader("y\n"), Output: &output, IsTerminal: func(io.Reader) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := policy.NewApprovalRequestForPurpose(
		"permission-1", "execute_command", json.RawMessage(`{"command":"touch /outside/file"}`),
		policy.ApprovalPurposePermission, policy.CommandRiskHigh, policy.ApprovalCause{Kind: policy.ApprovalCauseFilesystemRead, Code: "outside_workspace"},
	)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := prompt.Decide(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != policy.ApprovalAllow || decision.Scope != policy.ApprovalOnce || decision.Source != policy.ApprovalSourceUser {
		t.Fatalf("unexpected permission decision: %#v", decision)
	}
	if !strings.Contains(output.String(), "[y] once / [s] session / [n] no") {
		t.Fatalf("permission prompt omitted once scope: %q", output.String())
	}
}

func TestFullscreenApprovalPermissionChoicesUseOnceThenSession(t *testing.T) {
	request, err := policy.NewApprovalRequestForPurpose(
		"permission-1", "execute_command", json.RawMessage(`{"command":"touch /outside/file"}`),
		policy.ApprovalPurposePermission, policy.CommandRiskHigh, policy.ApprovalCause{Kind: policy.ApprovalCauseFilesystemRead, Code: "outside_workspace"},
	)
	if err != nil {
		t.Fatal(err)
	}
	choices := approvalChoices(request)
	if len(choices) != 3 {
		t.Fatalf("unexpected permission choices: %#v", choices)
	}
	if choices[0].decision.Outcome != policy.ApprovalAllow || choices[0].decision.Scope != policy.ApprovalOnce || !strings.Contains(choices[0].description, "once") {
		t.Fatalf("unexpected once choice: %#v", choices[0])
	}
	if choices[1].decision.Outcome != policy.ApprovalAllow || choices[1].decision.Scope != policy.ApprovalSession || !strings.Contains(choices[1].label, "this session") {
		t.Fatalf("unexpected session choice: %#v", choices[1])
	}
	if choices[2].decision.Outcome != policy.ApprovalDeny || choices[2].decision.Scope != policy.ApprovalOnce {
		t.Fatalf("unexpected deny choice: %#v", choices[2])
	}
}

func inlineApprovalRequest(t *testing.T, reason string) policy.ApprovalRequest {
	t.Helper()
	request, err := policy.NewApprovalRequest("approval-1", "write_file", json.RawMessage(`{"path":"a.txt"}`), policy.CommandRiskHigh, policy.ApprovalCause{Kind: policy.ApprovalCausePolicy, Code: "test", Detail: reason})
	if err != nil {
		t.Fatalf("create approval request: %v", err)
	}
	return request
}
