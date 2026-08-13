package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/policy"
)

type sessionRequestPort interface {
	Request(context.Context, protocol.InteractiveRequest) (protocol.Op, error)
}

type sessionApprovalPort struct {
	requester sessionRequestPort
}

func newSessionApprovalPort(requester sessionRequestPort) (*sessionApprovalPort, error) {
	if requester == nil {
		return nil, errors.New("session approval requester is nil")
	}
	return &sessionApprovalPort{requester: requester}, nil
}

func (port *sessionApprovalPort) Decide(ctx context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	if port == nil || port.requester == nil {
		return policy.ApprovalDecision{}, errors.New("session approval port is nil")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return policy.ApprovalDecision{}, fmt.Errorf("encode approval request: %w", err)
	}
	interactive := protocol.InteractiveRequest{
		RequestID: request.ID,
		Kind:      protocol.RequestApproval,
		Approval: &protocol.ApprovalRequest{
			ID:       request.ID,
			ToolName: request.ToolName,
			Raw:      raw,
			Presentation: protocol.ApprovalPresentation{
				Title:       request.Presentation.Title,
				Description: request.Presentation.Question,
				Diff:        fmt.Sprint(request.Diff),
			},
		},
	}
	for _, option := range request.Presentation.Options {
		interactive.Approval.Presentation.Options = append(interactive.Approval.Presentation.Options, protocol.ApprovalOption{ID: option.ID, Label: option.Label})
	}
	op, err := port.requester.Request(ctx, interactive)
	if err != nil {
		return policy.ApprovalDecision{}, err
	}
	decision, ok := op.(protocol.ApprovalDecisionOp)
	if !ok {
		return policy.ApprovalDecision{}, fmt.Errorf("unexpected interactive response %T", op)
	}
	return policy.ApprovalDecision{
		OptionID: decision.OptionID,
		Outcome:  policy.ApprovalOutcome(decision.Outcome),
		Scope:    policy.ApprovalScope(decision.Scope),
		Source:   policy.ApprovalSource(decision.Source),
		Reason:   decision.Reason,
	}, nil
}

func approvalRequestFromInteractive(request protocol.InteractiveRequest) (policy.ApprovalRequest, error) {
	if request.Kind != protocol.RequestApproval || request.Approval == nil {
		return policy.ApprovalRequest{}, errors.New("interactive request is not an approval request")
	}
	if len(request.Approval.Raw) == 0 {
		return policy.ApprovalRequest{}, errors.New("interactive approval request has no raw policy request")
	}
	var result policy.ApprovalRequest
	if err := json.Unmarshal(request.Approval.Raw, &result); err != nil {
		return policy.ApprovalRequest{}, fmt.Errorf("decode interactive approval request: %w", err)
	}
	if err := result.Validate(); err != nil {
		return policy.ApprovalRequest{}, err
	}
	return result, nil
}

func approvalDecisionOp(requestID string, decision policy.ApprovalDecision) protocol.ApprovalDecisionOp {
	return protocol.ApprovalDecisionOp{
		RequestID: requestID,
		OptionID:  decision.OptionID,
		Outcome:   string(decision.Outcome),
		Scope:     string(decision.Scope),
		Source:    string(decision.Source),
		Reason:    decision.Reason,
	}
}
