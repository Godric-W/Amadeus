package event

import (
	"errors"
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestEventTypeValidation(t *testing.T) {
	validTypes := []Type{
		TypeTurnStarted,
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
		TypePlanUpdated,
		TypeVerificationDone,
		TypeReflectionDone,
		TypeEngineRunCompleted,
	}
	for _, eventType := range validTypes {
		if !eventType.Valid() {
			t.Fatalf("expected valid event type: %q", eventType)
		}
	}
	if Type("custom.event").Valid() {
		t.Fatal("unexpected valid custom event type")
	}
}

func TestNewErrorInfoCopiesSafeProviderMetadata(t *testing.T) {
	providerError := &llm.ProviderError{
		Kind:       llm.ProviderErrorRateLimit,
		StatusCode: 429,
		Code:       "rate_limit_exceeded",
		Param:      "model",
		RequestID:  "request_1",
		Message:    "slow down",
		Cause:      errors.New("raw transport details"),
	}
	info := NewErrorInfo(providerError)
	if info.Kind != llm.ProviderErrorRateLimit || info.StatusCode != 429 || info.Code != "rate_limit_exceeded" || info.Param != "model" || info.RequestID != "request_1" || info.Message != "slow down" {
		t.Fatalf("unexpected provider error info: %#v", info)
	}
}

func TestNewErrorInfoClassifiesGenericErrorAsUnknown(t *testing.T) {
	info := NewErrorInfo(errors.New("unexpected failure"))
	if info.Kind != llm.ProviderErrorUnknown || info.Message != "unexpected failure" {
		t.Fatalf("unexpected generic error info: %#v", info)
	}
}

func TestAgentEventPayloadsExposeStableTypes(t *testing.T) {
	events := []Event{
		ToolCallStarted{}, ToolCallCompleted{}, ApprovalRequested{}, ApprovalResolved{},
		StatusChanged{}, DiagnosticPublished{}, PlanUpdated{},
	}
	want := []Type{
		TypeToolCallStarted, TypeToolCallCompleted, TypeApprovalRequested, TypeApprovalResolved,
		TypeStatusChanged, TypeDiagnosticPublished, TypePlanUpdated,
	}
	for index, runtimeEvent := range events {
		if runtimeEvent.Type() != want[index] {
			t.Fatalf("unexpected event type at %d: got %q want %q", index, runtimeEvent.Type(), want[index])
		}
	}
}
