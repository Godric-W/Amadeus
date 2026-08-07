package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/policy"
)

func TestTerminalApprovalInteractiveChoices(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		outcome policy.ApprovalOutcome
		scope   policy.ApprovalScope
	}{
		{name: "once", input: "y\n", outcome: policy.ApprovalAllow, scope: policy.ApprovalOnce},
		{name: "session", input: "s\n", outcome: policy.ApprovalAllow, scope: policy.ApprovalSession},
		{name: "deny", input: "n\n", outcome: policy.ApprovalDeny, scope: policy.ApprovalOnce},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			handler, err := NewTerminalApprovalHandler(TerminalApprovalOptions{
				Input: strings.NewReader(test.input), Output: &output, IsTerminal: func(_ io.Reader) bool { return true },
			})
			if err != nil {
				t.Fatalf("create approval handler: %v", err)
			}
			decision, err := handler.Decide(context.Background(), testApprovalRequest(t))
			if err != nil {
				t.Fatalf("decide approval: %v", err)
			}
			if decision.Outcome != test.outcome || decision.Scope != test.scope || decision.Source != policy.ApprovalSourceUser {
				t.Fatalf("unexpected decision: %#v", decision)
			}
			if !strings.Contains(output.String(), "[y] once / [s] session / [n] deny") || strings.Contains(output.String(), "always") {
				t.Fatalf("unexpected prompt: %q", output.String())
			}
		})
	}
}

func TestTerminalApprovalNonTTYAlwaysDenies(t *testing.T) {
	handler, err := NewTerminalApprovalHandler(TerminalApprovalOptions{
		Input: strings.NewReader("y\n"), Output: &bytes.Buffer{}, IsTerminal: func(_ io.Reader) bool { return false },
	})
	if err != nil {
		t.Fatalf("create approval handler: %v", err)
	}
	decision, err := handler.Decide(context.Background(), testApprovalRequest(t))
	if err != nil {
		t.Fatalf("decide approval: %v", err)
	}
	if decision.Allowed() || decision.Source != policy.ApprovalSourcePolicy || !strings.Contains(decision.Reason, "requires a TTY") {
		t.Fatalf("unexpected non-TTY decision: %#v", decision)
	}
}

func TestTerminalApprovalRejectsInvalidChoiceThenAccepts(t *testing.T) {
	var output bytes.Buffer
	handler, err := NewTerminalApprovalHandler(TerminalApprovalOptions{
		Input: strings.NewReader("always\ny\n"), Output: &output, IsTerminal: func(_ io.Reader) bool { return true },
	})
	if err != nil {
		t.Fatalf("create approval handler: %v", err)
	}
	decision, err := handler.Decide(context.Background(), testApprovalRequest(t))
	if err != nil {
		t.Fatalf("decide approval: %v", err)
	}
	if !decision.Allowed() || !strings.Contains(output.String(), "Invalid choice. Enter y, s, or n") {
		t.Fatalf("unexpected retry result: decision=%#v output=%q", decision, output.String())
	}
}

func TestTerminalApprovalPermissionChoiceUsesRunScope(t *testing.T) {
	var output bytes.Buffer
	handler, err := NewTerminalApprovalHandler(TerminalApprovalOptions{
		Input: strings.NewReader("y\n"), Output: &output, IsTerminal: func(_ io.Reader) bool { return true },
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
	decision, err := handler.Decide(context.Background(), request)
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

func testApprovalRequest(t *testing.T) policy.ApprovalRequest {
	t.Helper()
	request, err := policy.NewApprovalRequest("approval-1", "write_file", json.RawMessage(`{"path":"a.txt"}`), policy.CommandRiskHigh, "writes a project file")
	if err != nil {
		t.Fatalf("create approval request: %v", err)
	}
	return request
}
