package exec

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/policy"
)

func approvalRequestFromEvent(event protocol.ApprovalRequestEvent) (policy.ApprovalRequest, error) {
	if len(event.Approval.Raw) == 0 {
		return policy.ApprovalRequest{}, errors.New("approval request event has no raw policy request")
	}
	var result policy.ApprovalRequest
	if err := json.Unmarshal(event.Approval.Raw, &result); err != nil {
		return policy.ApprovalRequest{}, fmt.Errorf("decode approval request event: %w", err)
	}
	if err := result.Validate(); err != nil {
		return policy.ApprovalRequest{}, err
	}
	return result, nil
}

func approvalDecisionOp(requestID protocol.RequestID, decision policy.ApprovalDecision) protocol.ApprovalDecisionOp {
	return protocol.ApprovalDecisionOp{
		RequestID: requestID,
		OptionID:  decision.OptionID,
		Outcome:   string(decision.Outcome),
		Scope:     string(decision.Scope),
		Source:    string(decision.Source),
		Reason:    decision.Reason,
	}
}
