package llm

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var providerErrorSensitiveValue = regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*bearer\s+\S+|\b(?:api[_-]?key|token|password)\s*[:=]\s*\S+|\bbearer\s+\S+`)

type ProviderErrorKind string

const (
	ProviderErrorAuthentication ProviderErrorKind = "authentication"
	ProviderErrorRateLimit      ProviderErrorKind = "rate_limit"
	ProviderErrorNetwork        ProviderErrorKind = "network"
	ProviderErrorCancelled      ProviderErrorKind = "cancelled"
	ProviderErrorTimeout        ProviderErrorKind = "timeout"
	ProviderErrorContextWindow  ProviderErrorKind = "context_window_exceeded"
	ProviderErrorInvalidRequest ProviderErrorKind = "invalid_request"
	ProviderErrorUnavailable    ProviderErrorKind = "unavailable"
	ProviderErrorProtocol       ProviderErrorKind = "protocol"
	ProviderErrorUnknown        ProviderErrorKind = "unknown"
)

type ProviderError struct {
	Kind              ProviderErrorKind
	StatusCode        int
	Code              string
	Param             string
	RequestID         string
	Message           string
	AdditionalDetails string
	Retryable         bool
	RetryDelay        time.Duration
	Cause             error
}

func (providerError *ProviderError) Error() string {
	kind := providerError.Kind
	if !kind.Valid() {
		kind = ProviderErrorUnknown
	}
	message := SanitizeProviderErrorText(providerError.Message)
	if message == "" {
		message = "provider request failed"
	}
	if providerError.Code == "" {
		return fmt.Sprintf("provider %s error: %s", kind, message)
	}
	return fmt.Sprintf("provider %s error (%s): %s", kind, providerError.Code, message)
}

func SanitizeProviderErrorText(value string) string {
	value = strings.TrimSpace(value)
	return providerErrorSensitiveValue.ReplaceAllString(value, "[REDACTED]")
}

func (providerError *ProviderError) Unwrap() error {
	return providerError.Cause
}

func AsProviderError(err error) (*ProviderError, bool) {
	var providerError *ProviderError
	if !errors.As(err, &providerError) || providerError == nil {
		return nil, false
	}
	return providerError, true
}

func IsRetryableProviderError(err error) bool {
	providerError, ok := AsProviderError(err)
	return ok && providerError.Retryable
}

func (kind ProviderErrorKind) Valid() bool {
	switch kind {
	case ProviderErrorAuthentication,
		ProviderErrorRateLimit,
		ProviderErrorNetwork,
		ProviderErrorCancelled,
		ProviderErrorTimeout,
		ProviderErrorContextWindow,
		ProviderErrorInvalidRequest,
		ProviderErrorUnavailable,
		ProviderErrorProtocol,
		ProviderErrorUnknown:
		return true
	default:
		return false
	}
}
