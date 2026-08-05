package websearch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxResponseBytes int64 = 4 << 20

func defaultHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	return &http.Client{Transport: transport}
}

func execute(ctx context.Context, client *http.Client, provider string, request *http.Request) ([]byte, error) {
	response, err := client.Do(request.WithContext(ctx))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, &Error{Kind: ErrorTimeout, Provider: provider, Err: ctx.Err()}
		}
		return nil, &Error{Kind: ErrorNetwork, Provider: provider, Err: err}
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if readErr != nil {
		return nil, &Error{Kind: ErrorNetwork, Provider: provider, Err: readErr}
	}
	if int64(len(body)) > maxResponseBytes {
		return nil, &Error{Kind: ErrorProtocol, Provider: provider, Err: fmt.Errorf("response exceeds %d bytes", maxResponseBytes)}
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return body, nil
	}
	kind := ErrorUpstream
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = ErrorAuthentication
	case http.StatusTooManyRequests:
		kind = ErrorRateLimit
	default:
		if response.StatusCode < 500 {
			kind = ErrorProtocol
		}
	}
	detail := strings.TrimSpace(string(body))
	if len(detail) > 256 {
		detail = detail[:256]
	}
	return nil, &Error{Kind: kind, Provider: provider, StatusCode: response.StatusCode, Err: errors.New(detail)}
}
