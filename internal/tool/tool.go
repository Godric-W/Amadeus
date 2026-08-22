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
	Interactions  UserInputRequester
	Snapshot      RequestSnapshot
}

type ContextTargetKind string

const (
	ContextTargetFile       ContextTargetKind = "file"
	ContextTargetDirectory  ContextTargetKind = "directory"
	ContextTargetCommandCWD ContextTargetKind = "command_cwd"
)

type ContextTarget struct {
	Path       string
	Kind       ContextTargetKind
	SideEffect SideEffect
}

func (target ContextTarget) Validate() error {
	if target.Path == "" {
		return errors.New("tool context target path is empty")
	}
	switch target.Kind {
	case ContextTargetFile, ContextTargetDirectory, ContextTargetCommandCWD:
	default:
		return errors.New("tool context target kind is invalid")
	}
	if !target.SideEffect.Valid() {
		return errors.New("tool context target side effect is invalid")
	}
	return nil
}

type TargetObserver interface {
	ObserveTarget(context.Context, ContextTarget, RequestSnapshot) error
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
	if toolContext.Invocation.SessionID.IsZero() || toolContext.Invocation.ThreadID.IsZero() || toolContext.Invocation.TurnID == "" {
		return errors.New("tool use context identity is incomplete")
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
	Target     *ContextTarget
	Permission PermissionEvaluation
}

func (prepared PreparedToolUse) Validate() error {
	if prepared.Invocation.Call.ID == "" || prepared.Invocation.Call.Name == "" {
		return errors.New("prepared tool use has an invalid invocation")
	}
	if prepared.Target != nil {
		if err := prepared.Target.Validate(); err != nil {
			return err
		}
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
