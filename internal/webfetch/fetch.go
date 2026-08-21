package webfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type HTTPFetcher struct {
	client      *http.Client
	maxBytes    int64
	minInterval time.Duration
	mutex       sync.Mutex
	lastRequest time.Time
}

func New(options Options) (*HTTPFetcher, error) {
	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if options.MaxRedirects < 0 {
		return nil, errors.New("web fetch redirect limit cannot be negative")
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	resolve := options.Resolve
	if resolve == nil {
		resolve = resolveHost
	}
	dial := options.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext
	}
	client := &http.Client{
		Transport: newPinnedTransport(resolve, dial, options.Proxy, options.TLSConfig),
		Timeout:   timeout,
	}
	client.CheckRedirect = redirectPolicy(options.MaxRedirects)
	return &HTTPFetcher{client: client, maxBytes: maxBytes, minInterval: options.MinInterval}, nil
}

func redirectPolicy(maxRedirects int) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, via []*http.Request) error {
		parsed, err := ParseURL(request.URL.String())
		if err != nil {
			return err
		}
		if len(via) == 0 {
			return errorOf(ErrorRedirectLimit, parsed.String(), "redirect has no source request")
		}
		if !sameHostname(via[0].URL, parsed) {
			return &Error{
				Kind:        ErrorRedirectApprovalNeeded,
				URL:         via[0].URL.String(),
				RedirectURL: parsed.String(),
				Err:         fmt.Errorf("web redirect requires approval for hostname %q: %s", parsed.Hostname(), parsed.String()),
			}
		}
		if len(via) > maxRedirects {
			return &Error{Kind: ErrorRedirectLimit, URL: via[0].URL.String(), RedirectURL: parsed.String(), Err: fmt.Errorf("web request exceeded redirect limit %d", maxRedirects)}
		}
		return nil
	}
}

func (fetcher *HTTPFetcher) Fetch(ctx context.Context, rawURL string) (Document, error) {
	if fetcher == nil || fetcher.client == nil {
		return Document{}, errors.New("web fetcher is nil")
	}
	if err := fetcher.wait(ctx); err != nil {
		return Document{}, classifyRequestError(rawURL, err)
	}
	parsed, err := ParseURL(rawURL)
	if err != nil {
		return Document{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Document{}, errorOf(ErrorInvalidURL, rawURL, "create web request: %v", err)
	}
	request.Header.Set("Accept", "text/html, text/markdown;q=0.95, text/plain;q=0.9, application/json;q=0.8")
	request.Header.Set("User-Agent", "Amadeus/1.0")

	response, err := fetcher.client.Do(request)
	if err != nil {
		return Document{}, classifyRequestError(parsed.String(), err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Document{}, &Error{Kind: ErrorHTTPStatus, URL: response.Request.URL.String(), StatusCode: response.StatusCode, Err: fmt.Errorf("fetch web URL returned HTTP %d", response.StatusCode)}
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, fetcher.maxBytes+1))
	if err != nil {
		return Document{}, &Error{Kind: ErrorRead, URL: response.Request.URL.String(), Err: fmt.Errorf("read web response: %w", err)}
	}
	partial := int64(len(content)) > fetcher.maxBytes
	if partial {
		content = content[:fetcher.maxBytes]
	}
	document, err := projectContent(response.Request.URL, response.Header.Get("Content-Type"), content, partial)
	if err != nil {
		var fetchError *Error
		if errors.As(err, &fetchError) && strings.TrimSpace(fetchError.URL) == "" {
			fetchError.URL = response.Request.URL.String()
		}
		return Document{}, err
	}
	document.Partial = partial
	document.Bytes = int64(len(content))
	return document, nil
}

func classifyRequestError(rawURL string, err error) error {
	if err == nil {
		return nil
	}
	var fetchError *Error
	if errors.As(err, &fetchError) {
		return fetchError
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Kind: ErrorCanceled, URL: rawURL, Err: context.Canceled}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Kind: ErrorTimeout, URL: rawURL, Err: fmt.Errorf("web fetch timed out: %w", context.DeadlineExceeded)}
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return &Error{Kind: ErrorTimeout, URL: rawURL, Err: fmt.Errorf("web fetch timed out: %w", err)}
	}
	var urlError *url.Error
	if errors.As(err, &urlError) && urlError.Err != nil {
		return &Error{Kind: ErrorNetwork, URL: rawURL, Err: fmt.Errorf("fetch web URL: %w", urlError.Err)}
	}
	return &Error{Kind: ErrorNetwork, URL: rawURL, Err: fmt.Errorf("fetch web URL: %w", err)}
}

func (fetcher *HTTPFetcher) wait(ctx context.Context) error {
	if fetcher.minInterval <= 0 {
		return nil
	}
	fetcher.mutex.Lock()
	defer fetcher.mutex.Unlock()
	wait := fetcher.lastRequest.Add(fetcher.minInterval).Sub(time.Now())
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	fetcher.lastRequest = time.Now()
	return nil
}
