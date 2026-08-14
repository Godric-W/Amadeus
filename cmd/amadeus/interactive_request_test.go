package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/policy"
)

type capturingSessionRequestPort struct {
	request protocol.InteractiveRequest
	result  protocol.Op
}

func (port *capturingSessionRequestPort) Request(_ context.Context, request protocol.InteractiveRequest) (protocol.Op, error) {
	port.request = request
	return port.result, nil
}

func TestSessionApprovalPortProjectsCompletePresentation(t *testing.T) {
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
	request.Presentation = policy.CommandApprovalPresentation("go test ./...", "/workspace/amadeus")
	requester := &capturingSessionRequestPort{result: protocol.ApprovalDecisionOp{
		RequestID: request.ID,
		OptionID:  "allow",
		Outcome:   string(policy.ApprovalAllow),
		Scope:     string(policy.ApprovalOnce),
		Source:    string(policy.ApprovalSourceUser),
	}}
	port, err := newSessionApprovalPort(requester)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := port.Decide(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	projected := requester.request.Approval
	if projected == nil {
		t.Fatal("approval projection is nil")
	}
	if projected.Presentation.Title != request.Presentation.Title || projected.Presentation.Description != request.Presentation.Question {
		t.Fatalf("presentation heading was not preserved: %#v", projected.Presentation)
	}
	if len(projected.Presentation.Details) != 2 || projected.Presentation.Details[0] != "Command: go test ./..." {
		t.Fatalf("presentation details were not preserved: %#v", projected.Presentation.Details)
	}
	if len(projected.Presentation.Options) != 3 || projected.Presentation.Options[1].Description != request.Presentation.Options[1].Description {
		t.Fatalf("option descriptions were not preserved: %#v", projected.Presentation.Options)
	}
	if projected.Presentation.Title == "" || projected.Presentation.Description == "" || len(projected.Presentation.Details) == 0 || len(projected.Presentation.Options) == 0 {
		t.Fatalf("structured client would depend on raw approval JSON: %#v", projected.Presentation)
	}
}
