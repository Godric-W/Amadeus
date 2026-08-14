package policy

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ApprovalCoordinator serializes interactive approval prompts and returns the
// user's decision. It deliberately owns no permission state and never applies
// grants; session-scoped permission state is managed by the caller.
type ApprovalCoordinator struct {
	approvalPort ApprovalPort
	mutex        sync.Mutex
}

func NewApprovalCoordinator(approvalPort ApprovalPort) (*ApprovalCoordinator, error) {
	if approvalPort == nil {
		return nil, errors.New("approval coordinator approval port is nil")
	}
	return &ApprovalCoordinator{approvalPort: approvalPort}, nil
}

func (coordinator *ApprovalCoordinator) Decide(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	if coordinator == nil || coordinator.approvalPort == nil {
		return ApprovalDecision{}, errors.New("approval coordinator is nil")
	}
	if err := request.Validate(); err != nil {
		return ApprovalDecision{}, fmt.Errorf("validate approval request: %w", err)
	}
	coordinator.mutex.Lock()
	defer coordinator.mutex.Unlock()
	decision, err := coordinator.approvalPort.Decide(ctx, request.Clone())
	if err != nil {
		return ApprovalDecision{}, fmt.Errorf("resolve approval: %w", err)
	}
	if err := decision.Validate(); err != nil {
		return ApprovalDecision{}, fmt.Errorf("validate approval decision: %w", err)
	}
	return decision, nil
}
