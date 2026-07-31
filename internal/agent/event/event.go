package event

import (
	"context"
	"errors"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
)

type Type string

const (
	TypeTurnStarted         Type = "turn.started"
	TypeTurnCompleted       Type = "turn.completed"
	TypeTextDelta           Type = "text.delta"
	TypeReasoningDelta      Type = "reasoning.delta"
	TypeToolCallStarted     Type = "tool_call.started"
	TypeToolCallCompleted   Type = "tool_call.completed"
	TypeApprovalRequested   Type = "approval.requested"
	TypeApprovalResolved    Type = "approval.resolved"
	TypeUsageUpdated        Type = "usage.updated"
	TypeStatusChanged       Type = "status.changed"
	TypeDiagnosticPublished Type = "diagnostic.published"
	TypeErrorOccurred       Type = "error.occurred"
	TypeEngineRunStarted    Type = "engine.run.started"
	TypeEngineStatusChanged Type = "engine.status.changed"
	TypeVerificationDone    Type = "engine.verification.completed"
	TypeReflectionDone      Type = "engine.reflection.completed"
	TypeEngineRunCompleted  Type = "engine.run.completed"
)

type Event interface {
	Type() Type
}

type Sink interface {
	Publish(context.Context, Event) error
}

type TurnStarted struct {
	TurnID string
	Model  llm.ModelInfo
}

func (TurnStarted) Type() Type {
	return TypeTurnStarted
}

type TextDelta struct {
	TurnID     string
	ResponseID string
	Delta      string
}

func (TextDelta) Type() Type {
	return TypeTextDelta
}

type ReasoningDelta struct {
	TurnID     string
	ResponseID string
	Delta      string
}

func (ReasoningDelta) Type() Type {
	return TypeReasoningDelta
}

type UsageUpdated struct {
	TurnID     string
	ResponseID string
	Usage      llm.Usage
}

func (UsageUpdated) Type() Type {
	return TypeUsageUpdated
}

type TurnCompleted struct {
	TurnID               string
	ResponseID           string
	RequestID            string
	FinishReason         llm.FinishReason
	ProviderFinishReason string
}

type ToolCallStarted struct {
	RunID    string
	CallID   string
	ToolName string
}

func (ToolCallStarted) Type() Type { return TypeToolCallStarted }

type ToolCallCompleted struct {
	RunID    string
	CallID   string
	ToolName string
	Success  bool
	Partial  bool
	Summary  string
	Duration time.Duration
}

func (ToolCallCompleted) Type() Type { return TypeToolCallCompleted }

type ApprovalRequested struct {
	RequestID string
	ToolName  string
	Risk      string
	Reason    string
}

func (ApprovalRequested) Type() Type { return TypeApprovalRequested }

type ApprovalResolved struct {
	RequestID string
	ToolName  string
	Outcome   string
	Scope     string
	Source    string
	Reason    string
}

func (ApprovalResolved) Type() Type { return TypeApprovalResolved }

type StatusChanged struct {
	Entity   string
	EntityID string
	From     string
	To       string
}

func (StatusChanged) Type() Type { return TypeStatusChanged }

type DiagnosticPublished struct {
	Severity string
	Code     string
	Message  string
}

func (DiagnosticPublished) Type() Type { return TypeDiagnosticPublished }

func (TurnCompleted) Type() Type {
	return TypeTurnCompleted
}

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
		return ErrorInfo{
			Kind:       providerError.Kind,
			StatusCode: providerError.StatusCode,
			Code:       providerError.Code,
			Param:      providerError.Param,
			RequestID:  providerError.RequestID,
			Message:    providerError.Message,
		}
	}
	return ErrorInfo{
		Kind:    llm.ProviderErrorUnknown,
		Message: err.Error(),
	}
}

type ErrorOccurred struct {
	TurnID string
	Error  ErrorInfo
}

type EngineRunStarted struct {
	RunID  string
	TaskID string
}

func (EngineRunStarted) Type() Type {
	return TypeEngineRunStarted
}

type EngineStatusChanged struct {
	RunID    string
	Entity   string
	EntityID string
	From     string
	To       string
}

func (EngineStatusChanged) Type() Type {
	return TypeEngineStatusChanged
}

type VerificationCompleted struct {
	RunID        string
	TaskID       string
	Passed       bool
	EvidenceGaps []string
}

func (VerificationCompleted) Type() Type {
	return TypeVerificationDone
}

type ReflectionCompleted struct {
	RunID   string
	TaskID  string
	Scope   string
	Verdict string
}

func (ReflectionCompleted) Type() Type {
	return TypeReflectionDone
}

type EngineRunCompleted struct {
	RunID      string
	Status     string
	StopReason string
	Reason     string
}

func (EngineRunCompleted) Type() Type {
	return TypeEngineRunCompleted
}

func (ErrorOccurred) Type() Type {
	return TypeErrorOccurred
}

func (eventType Type) Valid() bool {
	switch eventType {
	case TypeTurnStarted,
		TypeTurnCompleted,
		TypeTextDelta,
		TypeReasoningDelta,
		TypeToolCallStarted,
		TypeToolCallCompleted,
		TypeApprovalRequested,
		TypeApprovalResolved,
		TypeUsageUpdated,
		TypeStatusChanged,
		TypeDiagnosticPublished,
		TypeErrorOccurred,
		TypeEngineRunStarted,
		TypeEngineStatusChanged,
		TypeVerificationDone,
		TypeReflectionDone,
		TypeEngineRunCompleted:
		return true
	default:
		return false
	}
}
