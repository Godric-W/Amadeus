package exec

import (
	"encoding/json"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/policy"
)

func TestApprovalRequestFromEventPreservesPolicyRequest(t *testing.T) {
	request, err := policy.NewApprovalRequest(
		"approval-1",
		"execute_command",
		json.RawMessage(`{"command":"go test ./..."}`),
		policy.CommandRiskHigh,
		policy.ApprovalCause{Kind: policy.ApprovalCauseCommand, Code: "host_command", Detail: "unsandboxed internal detail"},
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Presentation = policy.CommandApprovalPresentation("go test ./...", "Run the project test suite", "/workspace/amadeus")
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	event := protocol.ApprovalRequestEvent{RequestID: protocol.RequestID(request.ID), Approval: protocol.ApprovalRequest{ID: protocol.RequestID(request.ID), ToolName: request.ToolName, Raw: raw}}
	projected, err := approvalRequestFromEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Presentation.Title != request.Presentation.Title || projected.Presentation.Question != request.Presentation.Question {
		t.Fatalf("presentation heading was not preserved: %#v", projected.Presentation)
	}
	if len(projected.Presentation.Details) != 3 || projected.Presentation.Details[0] != "Command: go test ./..." || projected.Presentation.Details[1] != "Description: Run the project test suite" {
		t.Fatalf("presentation details were not preserved: %#v", projected.Presentation.Details)
	}
	if len(projected.Presentation.Options) != 3 || projected.Presentation.Options[1].Description != request.Presentation.Options[1].Description {
		t.Fatalf("option descriptions were not preserved: %#v", projected.Presentation.Options)
	}
	if projected.Presentation.Title == "" || projected.Presentation.Question == "" || len(projected.Presentation.Details) == 0 || len(projected.Presentation.Options) == 0 {
		t.Fatalf("approval request was not preserved: %#v", projected.Presentation)
	}
}
