package event

import (
	"context"
	"errors"

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
		TypeErrorOccurred:
		return true
	default:
		return false
	}
}
