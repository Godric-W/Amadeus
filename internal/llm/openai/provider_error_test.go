package openai

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

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

func TestProviderRetryDelayParsesSupportedHeaders(t *testing.T) {
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		header http.Header
		want   time.Duration
	}{
		{name: "milliseconds", header: http.Header{"Retry-After-Ms": []string{"125.5"}}, want: 125500 * time.Microsecond},
		{name: "seconds", header: http.Header{"Retry-After": []string{"2.5"}}, want: 2500 * time.Millisecond},
		{name: "http date", header: http.Header{"Retry-After": []string{now.Add(3 * time.Second).Format(http.TimeFormat)}}, want: 3 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := providerRetryDelayAt(&http.Response{Header: test.header}, now); got != test.want {
				t.Fatalf("retry delay = %s, want %s", got, test.want)
			}
		})
	}
}

func TestRetryableProviderErrorMatrix(t *testing.T) {
	tests := []struct {
		kind       llm.ProviderErrorKind
		statusCode int
		want       bool
	}{
		{kind: llm.ProviderErrorRateLimit, want: true},
		{kind: llm.ProviderErrorNetwork, want: true},
		{kind: llm.ProviderErrorTimeout, want: true},
		{kind: llm.ProviderErrorUnavailable, want: true},
		{kind: llm.ProviderErrorAuthentication, want: false},
		{kind: llm.ProviderErrorInvalidRequest, want: false},
		{kind: llm.ProviderErrorProtocol, want: false},
		{kind: llm.ProviderErrorCancelled, want: false},
		{kind: llm.ProviderErrorUnknown, statusCode: http.StatusServiceUnavailable, want: true},
		{kind: llm.ProviderErrorUnknown, statusCode: http.StatusTeapot, want: false},
	}
	for _, test := range tests {
		if got := retryableProviderError(test.kind, test.statusCode); got != test.want {
			t.Fatalf("retryableProviderError(%q, %d) = %v, want %v", test.kind, test.statusCode, got, test.want)
		}
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

func TestNewProviderErrorStoresSafeUserMessage(t *testing.T) {
	providerError := newProviderError(
		llm.ProviderErrorNetwork,
		0,
		"",
		"",
		"token=secret-value",
		"request_789",
		0,
		nil,
	)
	if strings.Contains(providerError.Message, "secret-value") || strings.Contains(providerError.AdditionalDetails, "secret-value") {
		t.Fatalf("normalized provider error retained sensitive message: %#v", providerError)
	}
}
