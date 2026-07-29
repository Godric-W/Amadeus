package llm

import (
	"fmt"
	"strings"
)

type ProviderErrorKind string

const (
	ProviderErrorAuthentication ProviderErrorKind = "authentication"
	ProviderErrorRateLimit      ProviderErrorKind = "rate_limit"
	ProviderErrorNetwork        ProviderErrorKind = "network"
	ProviderErrorCancelled      ProviderErrorKind = "cancelled"
	ProviderErrorTimeout        ProviderErrorKind = "timeout"
	ProviderErrorInvalidRequest ProviderErrorKind = "invalid_request"
	ProviderErrorUnavailable    ProviderErrorKind = "unavailable"
	ProviderErrorProtocol       ProviderErrorKind = "protocol"
	ProviderErrorUnknown        ProviderErrorKind = "unknown"
)

type ProviderError struct {
	Kind       ProviderErrorKind
	StatusCode int
	Code       string
	Param      string
	RequestID  string
	Message    string
	Cause      error
}

func (providerError *ProviderError) Error() string {
	kind := providerError.Kind
	if !kind.Valid() {
		kind = ProviderErrorUnknown
	}
	message := strings.TrimSpace(providerError.Message)
	if message == "" {
		message = "provider request failed"
	}
	if providerError.Code == "" {
		return fmt.Sprintf("provider %s error: %s", kind, message)
	}
	return fmt.Sprintf("provider %s error (%s): %s", kind, providerError.Code, message)
}

func (providerError *ProviderError) Unwrap() error {
	return providerError.Cause
}

func (kind ProviderErrorKind) Valid() bool {
	switch kind {
	case ProviderErrorAuthentication,
		ProviderErrorRateLimit,
		ProviderErrorNetwork,
		ProviderErrorCancelled,
		ProviderErrorTimeout,
		ProviderErrorInvalidRequest,
		ProviderErrorUnavailable,
		ProviderErrorProtocol,
		ProviderErrorUnknown:
		return true
	default:
		return false
	}
}
