package openai

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"syscall"
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
	openaisdk "github.com/openai/openai-go/v3"
)

func TestNormalizeProviderErrorClassifiesAPIErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		code       string
		expected   llm.ProviderErrorKind
	}{
		{name: "authentication status", statusCode: http.StatusUnauthorized, expected: llm.ProviderErrorAuthentication},
		{name: "authentication code", statusCode: http.StatusBadRequest, code: "invalid_api_key", expected: llm.ProviderErrorAuthentication},
		{name: "rate limit status", statusCode: http.StatusTooManyRequests, expected: llm.ProviderErrorRateLimit},
		{name: "quota code", statusCode: http.StatusBadRequest, code: "insufficient_quota", expected: llm.ProviderErrorRateLimit},
		{name: "invalid request", statusCode: http.StatusUnprocessableEntity, expected: llm.ProviderErrorInvalidRequest},
		{name: "timeout", statusCode: http.StatusGatewayTimeout, expected: llm.ProviderErrorTimeout},
		{name: "unavailable", statusCode: http.StatusServiceUnavailable, expected: llm.ProviderErrorUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apiError := &openaisdk.Error{
				StatusCode: test.statusCode,
				Code:       test.code,
				Param:      "model",
				Message:    "provider message",
				Response: &http.Response{Header: http.Header{
					"X-Request-Id": []string{"request_123"},
				}},
			}
			normalized := normalizeProviderError(apiError)
			var providerError *llm.ProviderError
			if !errors.As(normalized, &providerError) {
				t.Fatalf("unexpected normalized error: %v", normalized)
			}
			if providerError.Kind != test.expected || providerError.StatusCode != test.statusCode {
				t.Fatalf("unexpected provider classification: %#v", providerError)
			}
			if providerError.Code != test.code || providerError.Param != "model" || providerError.RequestID != "request_123" || providerError.Message != "provider message" {
				t.Fatalf("provider details were not preserved: %#v", providerError)
			}
		})
	}
}

func TestNormalizeProviderErrorClassifiesContextAndNetwork(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected llm.ProviderErrorKind
	}{
		{name: "cancelled", err: context.Canceled, expected: llm.ProviderErrorCancelled},
		{name: "deadline", err: context.DeadlineExceeded, expected: llm.ProviderErrorTimeout},
		{name: "network", err: &url.Error{Op: "Post", URL: "https://provider.example/v1", Err: syscall.ECONNRESET}, expected: llm.ProviderErrorNetwork},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized := normalizeProviderError(test.err)
			var providerError *llm.ProviderError
			if !errors.As(normalized, &providerError) || providerError.Kind != test.expected {
				t.Fatalf("unexpected provider classification: %#v", normalized)
			}
			if !errors.Is(normalized, test.err) {
				t.Fatal("normalized provider error did not preserve cause")
			}
		})
	}
}

func TestNewProviderErrorClassifiesStreamCode(t *testing.T) {
	providerError := newProviderError(
		providerErrorKind(0, "rate_limit_exceeded"),
		0,
		"rate_limit_exceeded",
		"model",
		"slow down",
		"request_456",
		0,
		nil,
	)
	if providerError.Kind != llm.ProviderErrorRateLimit || providerError.Code != "rate_limit_exceeded" || providerError.RequestID != "request_456" {
		t.Fatalf("unexpected stream provider error: %#v", providerError)
	}
}
