package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	openaisdk "github.com/openai/openai-go/v3"
)

func normalizeProviderError(err error) error {
	if err == nil {
		return nil
	}
	var existing *llm.ProviderError
	if errors.As(err, &existing) {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return newProviderError(llm.ProviderErrorCancelled, 0, "", "", "request cancelled", "", 0, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newProviderError(llm.ProviderErrorTimeout, 0, "", "", "provider request timed out", "", 0, err)
	}

	var apiError *openaisdk.Error
	if errors.As(err, &apiError) {
		return newProviderError(
			providerErrorKind(apiError.StatusCode, apiError.Code),
			apiError.StatusCode,
			apiError.Code,
			apiError.Param,
			apiError.Message,
			providerRequestID(apiError.Response),
			providerRetryDelay(apiError.Response),
			err,
		)
	}

	var syntaxError *json.SyntaxError
	if errors.As(err, &syntaxError) {
		return newProviderError(llm.ProviderErrorProtocol, 0, "", "", "invalid provider response", "", 0, err)
	}
	if isNetworkError(err) {
		return newProviderError(llm.ProviderErrorNetwork, 0, "", "", "provider network request failed", "", 0, err)
	}
	return newProviderError(llm.ProviderErrorUnknown, 0, "", "", err.Error(), "", 0, err)
}

func newProviderError(kind llm.ProviderErrorKind, statusCode int, code, param, message, requestID string, retryDelay time.Duration, cause error) *llm.ProviderError {
	if !kind.Valid() {
		kind = providerErrorKind(statusCode, code)
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = providerErrorMessage(kind, statusCode)
	}
	message = llm.SanitizeProviderErrorText(message)
	return &llm.ProviderError{
		Kind:              kind,
		StatusCode:        statusCode,
		Code:              strings.TrimSpace(code),
		Param:             strings.TrimSpace(param),
		RequestID:         strings.TrimSpace(requestID),
		Message:           message,
		AdditionalDetails: message,
		Retryable:         retryableProviderError(kind, statusCode),
		RetryDelay:        retryDelay,
		Cause:             cause,
	}
}

func retryableProviderError(kind llm.ProviderErrorKind, statusCode int) bool {
	switch kind {
	case llm.ProviderErrorRateLimit, llm.ProviderErrorNetwork, llm.ProviderErrorTimeout, llm.ProviderErrorUnavailable:
		return true
	case llm.ProviderErrorUnknown:
		return statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
	default:
		return false
	}
}

func providerErrorKind(statusCode int, code string) llm.ProviderErrorKind {
	normalizedCode := strings.ToLower(strings.TrimSpace(code))
	switch {
	case strings.Contains(normalizedCode, "cancel"):
		return llm.ProviderErrorCancelled
	case strings.Contains(normalizedCode, "timeout"):
		return llm.ProviderErrorTimeout
	case strings.Contains(normalizedCode, "rate_limit"),
		strings.Contains(normalizedCode, "too_many_requests"),
		strings.Contains(normalizedCode, "quota"):
		return llm.ProviderErrorRateLimit
	case strings.Contains(normalizedCode, "auth"),
		strings.Contains(normalizedCode, "api_key"),
		strings.Contains(normalizedCode, "unauthorized"),
		strings.Contains(normalizedCode, "forbidden"):
		return llm.ProviderErrorAuthentication
	case strings.Contains(normalizedCode, "server_error"),
		strings.Contains(normalizedCode, "internal_error"),
		strings.Contains(normalizedCode, "service_unavailable"):
		return llm.ProviderErrorUnavailable
	case strings.Contains(normalizedCode, "context_length"),
		strings.Contains(normalizedCode, "context_window"),
		strings.Contains(normalizedCode, "too_many_tokens"):
		return llm.ProviderErrorContextWindow
	case strings.Contains(normalizedCode, "invalid_request"),
		strings.Contains(normalizedCode, "invalid_parameter"):
		return llm.ProviderErrorInvalidRequest
	}

	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return llm.ProviderErrorAuthentication
	case statusCode == http.StatusTooManyRequests:
		return llm.ProviderErrorRateLimit
	case statusCode == http.StatusRequestTimeout || statusCode == http.StatusGatewayTimeout:
		return llm.ProviderErrorTimeout
	case statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError:
		return llm.ProviderErrorInvalidRequest
	case statusCode >= http.StatusInternalServerError:
		return llm.ProviderErrorUnavailable
	default:
		return llm.ProviderErrorUnknown
	}
}

func providerRequestID(response *http.Response) string {
	if response == nil {
		return ""
	}
	if requestID := response.Header.Get("x-request-id"); requestID != "" {
		return requestID
	}
	return response.Header.Get("request-id")
}

func providerRetryDelay(response *http.Response) time.Duration {
	return providerRetryDelayAt(response, time.Now())
}

func providerRetryDelayAt(response *http.Response, now time.Time) time.Duration {
	if response == nil {
		return 0
	}
	if milliseconds, err := strconv.ParseFloat(strings.TrimSpace(response.Header.Get("retry-after-ms")), 64); err == nil && milliseconds >= 0 {
		return time.Duration(milliseconds * float64(time.Millisecond))
	}
	retryAfter := strings.TrimSpace(response.Header.Get("Retry-After"))
	if retryAfter == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(retryAfter, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	when, err := http.ParseTime(retryAfter)
	if err != nil {
		return 0
	}
	delay := when.Sub(now)
	if delay < 0 {
		return 0
	}
	return delay
}

func providerErrorMessage(kind llm.ProviderErrorKind, statusCode int) string {
	if statusCode > 0 {
		if statusText := http.StatusText(statusCode); statusText != "" {
			return statusText
		}
	}
	switch kind {
	case llm.ProviderErrorAuthentication:
		return "provider authentication failed"
	case llm.ProviderErrorRateLimit:
		return "provider rate limit exceeded"
	case llm.ProviderErrorNetwork:
		return "provider network request failed"
	case llm.ProviderErrorCancelled:
		return "request cancelled"
	case llm.ProviderErrorTimeout:
		return "provider request timed out"
	case llm.ProviderErrorContextWindow:
		return "provider context window exceeded"
	case llm.ProviderErrorInvalidRequest:
		return "provider rejected the request"
	case llm.ProviderErrorUnavailable:
		return "provider is unavailable"
	case llm.ProviderErrorProtocol:
		return "invalid provider response"
	default:
		return "provider request failed"
	}
}

func isNetworkError(err error) bool {
	var networkError net.Error
	if errors.As(err, &networkError) {
		return true
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF)
}
