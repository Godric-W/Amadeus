package tool

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/policy"
)

// ToolUseContext contains the runtime services available to one tool use. It
// is intentionally separate from context.Context so prepared state and
// session-scoped capabilities are explicit and type-checkable.
type ToolUseContext struct {
	Context       context.Context
	Invocation    Invocation
	Permissions   *policy.SessionPermissionContext
	FileReadState *FileReadStateStore
	Snapshot      RequestSnapshot
}

func (toolContext ToolUseContext) Validate() error {
	if toolContext.Context == nil {
		return errors.New("tool use context has no context")
	}
	if toolContext.Invocation.Call.ID == "" {
		return errors.New("tool use context has no call ID")
	}
	if toolContext.Invocation.Call.Name == "" {
		return errors.New("tool use context has no tool name")
	}
	return nil
}

// PreparedToolUse is the immutable hand-off between preparation, permission
// evaluation, approval, and execution. State is private to the definition
// that produced it; the runtime never reconstructs it from raw model input.
type PreparedToolUse struct {
	Invocation Invocation
	Input      any
	State      any
	Permission PermissionEvaluation
}

func (prepared PreparedToolUse) Validate() error {
	if prepared.Invocation.Call.ID == "" || prepared.Invocation.Call.Name == "" {
		return errors.New("prepared tool use has an invalid invocation")
	}
	return prepared.Permission.Validate()
}

// ToolDefinition is the only tool contract understood by ToolExecutionService.
// NormalizeInput is owned by the service's schema validator; definitions own
// objective validation, side-effect-free preparation, and prepared execution.
type ToolDefinition interface {
	Spec() ToolSpec
	SupportsParallelToolCalls() bool
	ValidateInput(ToolUseContext, Invocation) error
	Prepare(ToolUseContext, Invocation) (PreparedToolUse, error)
	Execute(ToolUseContext, PreparedToolUse) (ToolResult, error)
}
