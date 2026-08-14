package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/policy"
)

type sessionApprovalPort struct{ requester eventRequestHost }

func newSessionApprovalPort(requester eventRequestHost) (*sessionApprovalPort, error) {
	if requester == nil {
		return nil, errors.New("session approval requester is nil")
	}
	return &sessionApprovalPort{requester: requester}, nil
}

func (port *sessionApprovalPort) Decide(ctx context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return policy.ApprovalDecision{}, fmt.Errorf("encode approval request: %w", err)
	}
	interactive := protocol.InteractiveRequest{
		RequestID: request.ID,
		Kind:      protocol.RequestApproval,
		Approval: &protocol.ApprovalRequest{
			ID: request.ID, ToolName: request.ToolName, Raw: raw,
			Presentation: protocol.ApprovalPresentation{
				Title: request.Presentation.Title, Description: request.Presentation.Question,
				Details: append([]string(nil), request.Presentation.Details...), Diff: request.Diff,
			},
		},
	}
	for _, option := range request.Presentation.Options {
		interactive.Approval.Presentation.Options = append(interactive.Approval.Presentation.Options, protocol.ApprovalOption{ID: option.ID, Label: option.Label, Description: option.Description})
	}
	op, err := port.requester.Request(ctx, interactive)
	if err != nil {
		return policy.ApprovalDecision{}, err
	}
	decision, ok := op.(protocol.ApprovalDecisionOp)
	if !ok {
		return policy.ApprovalDecision{}, fmt.Errorf("unexpected interactive response %T", op)
	}
	result := policy.ApprovalDecision{
		OptionID: decision.OptionID, Outcome: policy.ApprovalOutcome(decision.Outcome), Scope: policy.ApprovalScope(decision.Scope),
		Source: policy.ApprovalSource(decision.Source), Reason: decision.Reason,
	}
	if err := result.Validate(); err != nil {
		return policy.ApprovalDecision{}, err
	}
	return result, nil
}
