package event

import (
	"context"
	"errors"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
)

type Type string

const (
	TypeLLMCallStarted       Type = "llm_call.started"
	TypeLLMCallCompleted     Type = "llm_call.completed"
	TypeTextDelta            Type = "text.delta"
	TypeReasoningDelta       Type = "reasoning.delta"
	TypeToolCallStarted      Type = "tool_call.started"
	TypeToolCallCompleted    Type = "tool_call.completed"
	TypeApprovalRequested    Type = "approval.requested"
	TypeApprovalResolved     Type = "approval.resolved"
	TypeUsageUpdated         Type = "usage.updated"
	TypeContextWindowUpdated Type = "context_window.updated"
	TypeStatusChanged        Type = "status.changed"
	TypeDiagnosticPublished  Type = "diagnostic.published"
	TypeErrorOccurred        Type = "error.occurred"
	TypeRunStarted           Type = "run.started"
	TypeRunStatusChanged     Type = "run.status.changed"
	TypePlanUpdated          Type = "plan.updated"
	TypeRunDiffUpdated       Type = "run_diff.updated"
	TypeRunDiffInvalidated   Type = "run_diff.invalidated"
	TypeRunCompleted         Type = "run.completed"
	TypeIterationStarted     Type = "iteration.started"
	TypeIterationCompleted   Type = "iteration.completed"
)

type Metadata struct {
	SessionID string `json:"session_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	Iteration int    `json:"iteration,omitempty"`
	LLMCallID string `json:"llm_call_id,omitempty"`
}

type Event interface{ Type() Type }
type Sink interface {
	Publish(context.Context, Event) error
}

type LLMCallStarted struct {
	SessionID string
	RunID     string
	TaskID    string
	Iteration int
	LLMCallID string
	Model     llm.ModelInfo
}

func (LLMCallStarted) Type() Type { return TypeLLMCallStarted }

type TextDelta struct {
	SessionID  string
	RunID      string
	TaskID     string
	Iteration  int
	LLMCallID  string
	ResponseID string
	Delta      string
}

func (TextDelta) Type() Type { return TypeTextDelta }

type ReasoningDelta struct {
	SessionID  string
	RunID      string
	TaskID     string
	Iteration  int
	LLMCallID  string
	ResponseID string
	Delta      string
}

func (ReasoningDelta) Type() Type { return TypeReasoningDelta }

type UsageUpdated struct {
	SessionID  string
	RunID      string
	TaskID     string
	Iteration  int
	LLMCallID  string
	ResponseID string
	Usage      llm.Usage
}

func (UsageUpdated) Type() Type { return TypeUsageUpdated }

type ContextWindowUpdated struct {
	SessionID            string
	RunID                string
	TaskID               string
	Iteration            int
	LLMCallID            string
	EstimatedInputTokens int64
	ContextWindow        int64
	EffectiveInputLimit  int64
	ProjectedToolResults int
	DroppedMessagePairs  int
}

func (ContextWindowUpdated) Type() Type { return TypeContextWindowUpdated }

type LLMCallCompleted struct {
	SessionID            string
	RunID                string
	TaskID               string
	Iteration            int
	LLMCallID            string
	ResponseID           string
	RequestID            string
	FinishReason         llm.FinishReason
	ProviderFinishReason string
}

func (LLMCallCompleted) Type() Type { return TypeLLMCallCompleted }

type IterationStarted struct {
	SessionID string
	RunID     string
	TaskID    string
	Iteration int
	LLMCallID string
}

func (IterationStarted) Type() Type { return TypeIterationStarted }

type IterationCompleted struct {
	SessionID string
	RunID     string
	TaskID    string
	Iteration int
	LLMCallID string
	Status    string
	Reason    string
}

func (IterationCompleted) Type() Type { return TypeIterationCompleted }

type ToolCallStarted struct {
	SessionID     string
	RunID         string
	TaskID        string
	Iteration     int
	LLMCallID     string
	CallID        string
	ToolName      string
	SideEffect    string
	ActionSummary string
	Detail        string
}

func (ToolCallStarted) Type() Type { return TypeToolCallStarted }

type ToolCallCompleted struct {
	SessionID string
	RunID     string
	TaskID    string
	Iteration int
	LLMCallID string
	CallID    string
	ToolName  string
	Success   bool
	Partial   bool
	Summary   string
	Duration  time.Duration
}

func (ToolCallCompleted) Type() Type { return TypeToolCallCompleted }

type ApprovalRequested struct {
	SessionID string
	RunID     string
	TaskID    string
	Iteration int
	LLMCallID string
	RequestID string
	ToolName  string
	Risk      string
	Reason    string
}

func (ApprovalRequested) Type() Type { return TypeApprovalRequested }

type ApprovalResolved struct {
	SessionID string
	RunID     string
	TaskID    string
	Iteration int
	LLMCallID string
	RequestID string
	ToolName  string
	Outcome   string
	Scope     string
	Source    string
	Reason    string
}

func (ApprovalResolved) Type() Type { return TypeApprovalResolved }

type StatusChanged struct {
	SessionID string
	RunID     string
	TaskID    string
	Iteration int
	LLMCallID string
	Entity    string
	EntityID  string
	From      string
	To        string
}

func (StatusChanged) Type() Type { return TypeStatusChanged }

type DiagnosticPublished struct {
	SessionID string
	RunID     string
	TaskID    string
	Iteration int
	LLMCallID string
	Severity  string
	Code      string
	Message   string
}

func (DiagnosticPublished) Type() Type { return TypeDiagnosticPublished }

type ErrorInfo struct {
	Kind       llm.ProviderErrorKind
	StatusCode int
	Code       string
	Param      string
	RequestID  string
	Message    string
}

func NewErrorInfo(err error) ErrorInfo {
	if err == nil {
		return ErrorInfo{}
	}
	var providerError *llm.ProviderError
	if errors.As(err, &providerError) {
		return ErrorInfo{Kind: providerError.Kind, StatusCode: providerError.StatusCode, Code: providerError.Code, Param: providerError.Param, RequestID: providerError.RequestID, Message: providerError.Message}
	}
	return ErrorInfo{Kind: llm.ProviderErrorUnknown, Message: err.Error()}
}

type ErrorOccurred struct {
	SessionID string
	RunID     string
	TaskID    string
	Iteration int
	LLMCallID string
	Error     ErrorInfo
}

func (ErrorOccurred) Type() Type { return TypeErrorOccurred }

type RunStarted struct {
	SessionID string
	RunID     string
	TaskID    string
	Iteration int
	LLMCallID string
}

func (RunStarted) Type() Type { return TypeRunStarted }

type RunStatusChanged struct {
	SessionID string
	RunID     string
	TaskID    string
	Iteration int
	LLMCallID string
	Entity    string
	EntityID  string
	From      string
	To        string
}

func (RunStatusChanged) Type() Type { return TypeRunStatusChanged }

type PlanItem struct {
	Step   string
	Status string
}
type PlanUpdated struct {
	SessionID   string
	RunID       string
	TaskID      string
	Iteration   int
	LLMCallID   string
	Explanation string
	Items       []PlanItem
	Revision    int64
	UpdatedAt   time.Time
}

func (PlanUpdated) Type() Type { return TypePlanUpdated }

type RunDiffChange struct {
	Path         string
	PreviousPath string
	Kind         string
	Bytes        int
}

type RunDiffUpdated struct {
	SessionID string
	RunID     string
	TaskID    string
	Iteration int
	LLMCallID string
	Revision  int64
	Changes   []RunDiffChange
}

func (RunDiffUpdated) Type() Type { return TypeRunDiffUpdated }

type RunDiffInvalidated struct {
	SessionID string
	RunID     string
	TaskID    string
	Iteration int
	LLMCallID string
	Revision  int64
	Reason    string
}

func (RunDiffInvalidated) Type() Type { return TypeRunDiffInvalidated }

type RunCompleted struct {
	SessionID  string
	RunID      string
	TaskID     string
	Iteration  int
	LLMCallID  string
	Status     string
	StopReason string
	Reason     string
}

func (RunCompleted) Type() Type { return TypeRunCompleted }

func (eventType Type) Valid() bool {
	switch eventType {
	case TypeLLMCallStarted, TypeLLMCallCompleted, TypeTextDelta, TypeReasoningDelta,
		TypeToolCallStarted, TypeToolCallCompleted, TypeApprovalRequested, TypeApprovalResolved,
		TypeUsageUpdated, TypeContextWindowUpdated, TypeStatusChanged, TypeDiagnosticPublished, TypeErrorOccurred,
		TypeRunStarted, TypeRunStatusChanged, TypePlanUpdated, TypeRunDiffUpdated, TypeRunDiffInvalidated,
		TypeRunCompleted, TypeIterationStarted, TypeIterationCompleted:
		return true
	default:
		return false
	}
}
