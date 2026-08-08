package policy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Godric-W/Amadeus/internal/audit"
	sandboxdomain "github.com/Godric-W/Amadeus/internal/sandbox"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestCommandAuthorizerAuditsUnsandboxedApproval(t *testing.T) {
	sink := audit.NewMemorySink()
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{{Outcome: ApprovalAllow, Scope: ApprovalOnce, Source: ApprovalSourceUser, Reason: "approved"}}}
	authorizer, err := NewCommandAuthorizerWithOptions(handler, CommandAuthorizerOptions{SessionApprovals: NewSessionApprovalStore(), Audit: sink, SessionID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	call := tool.NewCall("cmd-1", "execute_command", json.RawMessage(`{"command":"go test ./..."}`))
	if err := authorizer.Authorize(context.Background(), CommandRequest{Call: call, Command: "go test ./...", Shell: "/bin/sh", CWD: "/tmp", IsolationMode: sandboxdomain.IsolationUnsandboxed}); err != nil {
		t.Fatal(err)
	}
	records := sink.Snapshot()
	if len(records) != 1 || records[0].Outcome != audit.OutcomeAllow || records[0].ArgumentsSHA256 == "" {
		t.Fatalf("unexpected audit: %#v", records)
	}
}
