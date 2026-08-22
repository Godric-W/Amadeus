package protocol

import (
	"testing"

	"github.com/Godric-W/Amadeus/internal/testutil"
)

func TestEventValidate(t *testing.T) {
	if err := (Event{}).Validate(); err == nil {
		t.Fatal("expected an empty event to be rejected")
	}
	if err := (Event{ID: "submission-1"}).Validate(); err == nil {
		t.Fatal("expected an event without a message to be rejected")
	}
}

func TestApprovalRequestEventValidateTypedPayload(t *testing.T) {
	request := ApprovalRequestEvent{
		RequestID: "request-1",
		ThreadID:  testutil.ThreadID(1),
		TurnID:    "turn-1",
		Approval:  ApprovalRequest{ID: "request-1", ToolName: "edit"},
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("valid approval request rejected: %v", err)
	}

	request.Approval = ApprovalRequest{}
	if err := request.Validate(); err == nil {
		t.Fatal("expected approval request without payload to be rejected")
	}
}

func TestApprovalDecisionOpCarriesFullDecision(t *testing.T) {
	decision := ApprovalDecisionOp{RequestID: "request-1", OptionID: "allow-session", Outcome: "allow", Scope: "session", Source: "user", Reason: "approved"}
	if decision.RequestID == "" || decision.OptionID != "allow-session" || decision.Outcome != "allow" || decision.Scope != "session" {
		t.Fatalf("unexpected approval decision: %#v", decision)
	}
}
