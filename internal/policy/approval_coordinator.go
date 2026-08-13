package policy

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/Godric-W/Amadeus/internal/agent/event"
)

// ApprovalCoordinator is the single runtime boundary between a tool's
// approval request and the UI approval port. It owns validation, lifecycle
// events, and the in-memory session grant update.
type ApprovalCoordinator struct {
	approvalPort ApprovalPort
	permissions  *SessionPermissionContext
	events       event.Sink
	mutex        sync.Mutex
}

func NewApprovalCoordinator(approvalPort ApprovalPort, permissions *SessionPermissionContext, events event.Sink) (*ApprovalCoordinator, error) {
	if approvalPort == nil {
		return nil, errors.New("approval coordinator approval port is nil")
	}
	if permissions == nil {
		permissions = NewSessionPermissionContext()
	}
	return &ApprovalCoordinator{approvalPort: approvalPort, permissions: permissions, events: events}, nil
}

func (coordinator *ApprovalCoordinator) Permissions() *SessionPermissionContext {
	if coordinator == nil {
		return nil
	}
	return coordinator.permissions
}

func (coordinator *ApprovalCoordinator) Decide(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	return coordinator.DecideForGrant(ctx, request, grantForRequest(request))
}

func (coordinator *ApprovalCoordinator) DecideForGrant(ctx context.Context, request ApprovalRequest, grant PermissionGrant) (ApprovalDecision, error) {
	if coordinator == nil || coordinator.approvalPort == nil {
		return ApprovalDecision{}, errors.New("approval coordinator is nil")
	}
	if err := request.Validate(); err != nil {
		return ApprovalDecision{}, fmt.Errorf("validate approval request: %w", err)
	}
	if !grant.Valid() {
		return ApprovalDecision{}, errors.New("approval coordinator session grant is invalid")
	}
	coordinator.mutex.Lock()
	defer coordinator.mutex.Unlock()
	if coordinator.permissions.Match(grant) {
		return ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceGrant, Reason: "matching session permission grant"}, nil
	}
	if coordinator.events != nil {
		if err := coordinator.events.Publish(ctx, event.ApprovalRequested{RequestID: request.ID, ToolName: request.ToolName, Risk: string(request.Risk), Reason: request.Reason}); err != nil {
			return ApprovalDecision{}, fmt.Errorf("publish approval requested: %w", err)
		}
	}
	decision, err := coordinator.approvalPort.Decide(ctx, request.Clone())
	if err != nil {
		return ApprovalDecision{}, fmt.Errorf("resolve approval: %w", err)
	}
	if err := decision.Validate(); err != nil {
		return ApprovalDecision{}, fmt.Errorf("validate approval decision: %w", err)
	}
	if coordinator.events != nil {
		if err := coordinator.events.Publish(ctx, event.ApprovalResolved{RequestID: request.ID, ToolName: request.ToolName, Outcome: string(decision.Outcome), Scope: string(decision.Scope), Source: string(decision.Source), Reason: decision.Reason}); err != nil {
			return ApprovalDecision{}, fmt.Errorf("publish approval resolved: %w", err)
		}
	}
	if decision.Allowed() && decision.Scope == ApprovalSession {
		coordinator.permissions.ApplyGrant(grant)
	}
	return decision, nil
}

func grantForRequest(request ApprovalRequest) PermissionGrant {
	switch request.Purpose {
	case ApprovalPurposeFile:
		return FileDirectoryGrant(filepath.Dir(request.Path))
	case ApprovalPurposeCommand:
		key, ok := NewCommandApprovalKey(request.Command, request.CWD)
		if ok {
			return CommandGrant(key)
		}
	case ApprovalPurposeExternal:
		return ExternalGrant(request.PermissionKey)
	}
	return PermissionGrant{}
}
