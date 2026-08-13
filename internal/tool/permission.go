package tool

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/policy"
)

type PermissionDecision string

const (
	PermissionAllow PermissionDecision = "allow"
	PermissionAsk   PermissionDecision = "ask"
	PermissionDeny  PermissionDecision = "deny"
)

type PermissionCheck struct {
	Decision   PermissionDecision
	Request    *policy.ApprovalRequest
	Grant      policy.PermissionGrant
	Prepared   any
	Reason     string
	OnDecision func(context.Context, policy.ApprovalDecision) error
}

func (check PermissionCheck) Validate() error {
	switch check.Decision {
	case PermissionAllow:
		return nil
	case PermissionAsk:
		if check.Request == nil {
			return errors.New("permission ask has no approval request")
		}
		if err := check.Request.Validate(); err != nil {
			return err
		}
		if !check.Grant.Valid() {
			return errors.New("permission ask has no valid session grant")
		}
		return nil
	case PermissionDeny:
		return nil
	default:
		return errors.New("permission decision is invalid")
	}
}

type PermissionChecker interface {
	CheckPermissions(context.Context, Invocation) (PermissionCheck, error)
}

type permissionCheckContextKey struct{}

func WithPermissionCheck(ctx context.Context, check PermissionCheck) context.Context {
	return context.WithValue(ctx, permissionCheckContextKey{}, check)
}

func PermissionCheckFromContext(ctx context.Context) (PermissionCheck, bool) {
	if ctx == nil {
		return PermissionCheck{}, false
	}
	check, ok := ctx.Value(permissionCheckContextKey{}).(PermissionCheck)
	return check, ok
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
