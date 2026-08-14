package tool

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Godric-W/Amadeus/internal/policy"
)

type PermissionDecision string

const (
	PermissionAllow PermissionDecision = "allow"
	PermissionAsk   PermissionDecision = "ask"
	PermissionDeny  PermissionDecision = "deny"
)

// PermissionEvaluation is the prepared, side-effect-free permission request
// consumed by PermissionService. Tool preparation state remains exclusively
// in PreparedToolUse.
type PermissionEvaluation struct {
	Decision PermissionDecision
	Request  *policy.ApprovalRequest
	Grant    policy.PermissionGrant
	Reason   string
	Observe  func(context.Context, policy.ApprovalDecision) error
}

func AllowPermission() PermissionEvaluation {
	return PermissionEvaluation{Decision: PermissionAllow}
}

func (evaluation PermissionEvaluation) Validate() error {
	switch evaluation.Decision {
	case PermissionAllow, PermissionDeny:
		return nil
	case PermissionAsk:
		if evaluation.Request == nil {
			return errors.New("permission ask has no approval request")
		}
		if err := evaluation.Request.Validate(); err != nil {
			return err
		}
		if !evaluation.Grant.Valid() {
			return errors.New("permission ask has no valid session grant")
		}
		return nil
	default:
		return errors.New("permission decision is invalid")
	}
}

type PermissionService struct {
	permissions *policy.SessionPermissionContext
	approvals   *policy.ApprovalCoordinator
	mutex       sync.Mutex
}

func NewPermissionService(permissions *policy.SessionPermissionContext, approvals *policy.ApprovalCoordinator) *PermissionService {
	return &PermissionService{permissions: permissions, approvals: approvals}
}

func (service *PermissionService) Evaluate(ctx context.Context, toolName string, evaluation PermissionEvaluation) error {
	if err := evaluation.Validate(); err != nil {
		return fmt.Errorf("validate permission evaluation: %w", err)
	}
	observe := func(decision policy.ApprovalDecision) error {
		if evaluation.Observe == nil {
			return nil
		}
		return evaluation.Observe(ctx, decision)
	}
	switch evaluation.Decision {
	case PermissionAllow:
		return observe(policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceGrant, Reason: "permission allowed"})
	case PermissionDeny:
		decision := policy.ApprovalDecision{Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourcePolicy, Reason: evaluation.Reason}
		if err := observe(decision); err != nil {
			return err
		}
		return &PermissionDeniedError{ToolName: toolName, Reason: evaluation.Reason}
	case PermissionAsk:
		if service == nil || service.approvals == nil {
			return errors.New("tool permission requires approval coordinator")
		}
		service.mutex.Lock()
		defer service.mutex.Unlock()
		if service.permissions != nil && service.permissions.Match(evaluation.Grant) {
			return observe(policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalSession, Source: policy.ApprovalSourceGrant, Reason: "matching session permission grant"})
		}
		decision, err := service.approvals.Decide(ctx, *evaluation.Request)
		if err != nil {
			return err
		}
		if err := observe(decision); err != nil {
			return err
		}
		if !decision.Allowed() {
			return &PermissionDeniedError{ToolName: toolName, Reason: decision.Reason}
		}
		if decision.Scope == policy.ApprovalSession && service.permissions != nil {
			service.permissions.ApplyGrant(evaluation.Grant)
		}
		return nil
	default:
		return errors.New("unsupported tool permission decision")
	}
}

type PermissionDeniedError struct {
	ToolName string
	Reason   string
}

func (err *PermissionDeniedError) Error() string {
	if err.Reason == "" {
		return "tool permission denied: " + err.ToolName
	}
	return "tool permission denied: " + err.ToolName + ": " + err.Reason
}

func (err *PermissionDeniedError) ToolErrorKind() string { return "permission_denied" }
