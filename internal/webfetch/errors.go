package webfetch

import (
	"errors"
	"fmt"
	"strings"
)

type ErrorKind string

const (
	ErrorInvalidURL             ErrorKind = "invalid_url"
	ErrorSSRFRejected           ErrorKind = "ssrf_rejected"
	ErrorRedirectApprovalNeeded ErrorKind = "redirect_approval_required"
	ErrorRedirectLimit          ErrorKind = "redirect_limit"
	ErrorTimeout                ErrorKind = "timeout"
	ErrorCanceled               ErrorKind = "canceled"
	ErrorEmptyContent           ErrorKind = "empty_content"
	ErrorUnsupportedContent     ErrorKind = "unsupported_content"
	ErrorInvalidContent         ErrorKind = "invalid_content"
	ErrorHTTPStatus             ErrorKind = "http_status"
	ErrorNetwork                ErrorKind = "network"
	ErrorRead                   ErrorKind = "read_failed"
)

type Error struct {
	Kind        ErrorKind
	URL         string
	RedirectURL string
	ContentType string
	StatusCode  int
	Err         error
}

func (fetchError *Error) Error() string {
	if fetchError == nil {
		return "web fetch failed"
	}
	if fetchError.Err != nil {
		return fetchError.Err.Error()
	}
	return string(fetchError.Kind)
}

func (fetchError *Error) Unwrap() error {
	if fetchError == nil {
		return nil
	}
	return fetchError.Err
}

func (fetchError *Error) ToolErrorKind() string {
	if fetchError == nil {
		return "web_fetch_failed"
	}
	return string(fetchError.Kind)
}

func errorOf(kind ErrorKind, rawURL, format string, arguments ...any) *Error {
	return &Error{Kind: kind, URL: rawURL, Err: fmt.Errorf(format, arguments...)}
}

func Details(err error) map[string]any {
	if err == nil {
		return nil
	}
	details := map[string]any{"error": err.Error(), "error_kind": "web_fetch_failed"}
	var fetchError *Error
	if !errors.As(err, &fetchError) {
		return details
	}
	details["error_kind"] = string(fetchError.Kind)
	if strings.TrimSpace(fetchError.URL) != "" {
		details["url"] = fetchError.URL
	}
	if strings.TrimSpace(fetchError.RedirectURL) != "" {
		details["redirect_url"] = fetchError.RedirectURL
	}
	if strings.TrimSpace(fetchError.ContentType) != "" {
		details["content_type"] = fetchError.ContentType
	}
	if fetchError.StatusCode != 0 {
		details["status_code"] = fetchError.StatusCode
	}
	return details
}
