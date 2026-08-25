package llm

import (
	"errors"
	"strings"
	"testing"
)

func TestProviderErrorPreservesStructuredDetailsAndCause(t *testing.T) {
	cause := errors.New("transport failed")
	providerError := &ProviderError{
		Kind:       ProviderErrorRateLimit,
		StatusCode: 429,
		Code:       "rate_limit_exceeded",
		Param:      "model",
		RequestID:  "request_1",
		Message:    "slow down",
		Cause:      cause,
	}

	if !strings.Contains(providerError.Error(), "rate_limit") || !strings.Contains(providerError.Error(), "rate_limit_exceeded") {
		t.Fatalf("unexpected provider error text: %q", providerError.Error())
	}
	if !errors.Is(providerError, cause) {
		t.Fatal("provider error did not preserve cause")
	}
}

func TestProviderErrorKindValidation(t *testing.T) {
	validKinds := []ProviderErrorKind{
		ProviderErrorAuthentication,
		ProviderErrorRateLimit,
		ProviderErrorNetwork,
		ProviderErrorCancelled,
		ProviderErrorTimeout,
		ProviderErrorContextWindow,
		ProviderErrorInvalidRequest,
		ProviderErrorUnavailable,
		ProviderErrorProtocol,
		ProviderErrorUnknown,
	}
	for _, kind := range validKinds {
		if !kind.Valid() {
			t.Fatalf("expected valid provider error kind: %q", kind)
		}
	}
	if ProviderErrorKind("other").Valid() {
		t.Fatal("unexpected valid custom provider error kind")
	}
}

func TestProviderErrorTextRedactsSensitiveValues(t *testing.T) {
	providerError := &ProviderError{Kind: ProviderErrorNetwork, Message: "Authorization: Bearer secret-value"}
	if text := providerError.Error(); strings.Contains(text, "secret-value") || !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("provider error leaked sensitive value: %q", text)
	}
	if text := SanitizeProviderErrorText("token=secret-value"); strings.Contains(text, "secret-value") || !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("provider detail leaked sensitive value: %q", text)
	}
}
