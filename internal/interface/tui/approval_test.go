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
		if strings.Contains(output.String(), "secret-value") || !strings.Contains(output.String(), "[REDACTED]") || !strings.Contains(output.String(), "[y] once / [s] session / [n] deny") {
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
	if err != nil || !decision.Allowed() || !strings.Contains(output.String(), "Invalid choice. Enter y, s, or n") {
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
		"permission-1", "request_permissions", json.RawMessage(`{"writable_roots":["/outside"]}`),
		policy.ApprovalPurposePermission, policy.CommandRiskHigh, "write outside the workspace",
	)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := prompt.Decide(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != policy.ApprovalAllow || decision.Scope != policy.ApprovalRun || decision.Source != policy.ApprovalSourceUser {
		t.Fatalf("unexpected permission decision: %#v", decision)
	}
	if !strings.Contains(output.String(), "[y] this run / [s] session / [n] deny") {
		t.Fatalf("permission prompt omitted run scope: %q", output.String())
	}
}

func TestFullscreenApprovalPermissionChoicesUseRunThenSession(t *testing.T) {
	request, err := policy.NewApprovalRequestForPurpose(
		"permission-1", "request_permissions", json.RawMessage(`{"writable_roots":["/outside"]}`),
		policy.ApprovalPurposePermission, policy.CommandRiskHigh, "write outside the workspace",
	)
	if err != nil {
		t.Fatal(err)
	}
	choices := approvalChoices(request)
	if len(choices) != 3 {
		t.Fatalf("unexpected permission choices: %#v", choices)
	}
	if choices[0].decision.Outcome != policy.ApprovalAllow || choices[0].decision.Scope != policy.ApprovalRun || !strings.Contains(choices[0].label, "this run") {
		t.Fatalf("unexpected run choice: %#v", choices[0])
	}
	if choices[1].decision.Outcome != policy.ApprovalAllow || choices[1].decision.Scope != policy.ApprovalSession || !strings.Contains(choices[1].label, "this session") {
		t.Fatalf("unexpected session choice: %#v", choices[1])
	}
	if choices[2].decision.Outcome != policy.ApprovalDeny {
		t.Fatalf("unexpected deny choice: %#v", choices[2])
	}
}

func inlineApprovalRequest(t *testing.T, reason string) policy.ApprovalRequest {
	t.Helper()
	request, err := policy.NewApprovalRequest("approval-1", "write_file", json.RawMessage(`{"path":"a.txt"}`), policy.CommandRiskHigh, reason)
	if err != nil {
		t.Fatalf("create approval request: %v", err)
	}
	return request
}
