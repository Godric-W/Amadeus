package websearch

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type Provider interface {
	Search(context.Context, string, int) ([]Result, error)
}

type ErrorKind string

const (
	ErrorInvalidConfig  ErrorKind = "invalid_config"
	ErrorAuthentication ErrorKind = "authentication"
	ErrorRateLimit      ErrorKind = "rate_limit"
	ErrorUpstream       ErrorKind = "upstream"
	ErrorTimeout        ErrorKind = "timeout"
	ErrorProtocol       ErrorKind = "protocol"
	ErrorNetwork        ErrorKind = "network"
)

type Error struct {
	Kind       ErrorKind
	Provider   string
	StatusCode int
	Err        error
}

func (value *Error) Error() string {
	if value == nil {
		return "web search failed"
	}
	detail := ""
	if value.Err != nil {
		detail = ": " + value.Err.Error()
	}
	if value.StatusCode != 0 {
		return fmt.Sprintf("web search provider %s failed (%s, HTTP %d)%s", value.Provider, value.Kind, value.StatusCode, detail)
	}
	return fmt.Sprintf("web search provider %s failed (%s)%s", value.Provider, value.Kind, detail)
}

func (value *Error) Unwrap() error { return value.Err }

type ServiceOptions struct {
	Timeout    time.Duration
	MaxResults int
	RetryDelay time.Duration
}

type Service struct {
	provider Provider
	options  ServiceOptions
}

func NewService(provider Provider, options ServiceOptions) (*Service, error) {
	if provider == nil {
		return nil, errors.New("web search provider is nil")
	}
	if options.Timeout <= 0 {
		options.Timeout = 15 * time.Second
	}
	if options.MaxResults <= 0 {
		options.MaxResults = 5
	}
	if options.MaxResults > 10 {
		options.MaxResults = 10
	}
	if options.RetryDelay <= 0 {
		options.RetryDelay = 100 * time.Millisecond
	}
	return &Service{provider: provider, options: options}, nil
}

func (service *Service) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("web search query is empty")
	}
	if limit <= 0 || limit > service.options.MaxResults {
		limit = service.options.MaxResults
	}
	requestContext, cancel := context.WithTimeout(ctx, service.options.Timeout)
	defer cancel()
	results, err := service.provider.Search(requestContext, query, limit)
	if err != nil && transient(err) && requestContext.Err() == nil {
		timer := time.NewTimer(service.options.RetryDelay)
		select {
		case <-requestContext.Done():
			timer.Stop()
			return nil, classifyContextError(requestContext.Err(), err)
		case <-timer.C:
		}
		results, err = service.provider.Search(requestContext, query, limit)
	}
	if err != nil {
		return nil, classifyContextError(requestContext.Err(), err)
	}
	return normalizeResults(results, limit), nil
}

func transient(err error) bool {
	var searchErr *Error
	if !errors.As(err, &searchErr) {
		return true
	}
	return searchErr.Kind == ErrorNetwork || searchErr.Kind == ErrorRateLimit || searchErr.Kind == ErrorUpstream
}

func classifyContextError(contextErr, err error) error {
	if errors.Is(contextErr, context.DeadlineExceeded) {
		var searchErr *Error
		if errors.As(err, &searchErr) {
			return &Error{Kind: ErrorTimeout, Provider: searchErr.Provider, Err: context.DeadlineExceeded}
		}
		return &Error{Kind: ErrorTimeout, Provider: "unknown", Err: context.DeadlineExceeded}
	}
	return err
}

func normalizeResults(values []Result, limit int) []Result {
	results := make([]Result, 0, min(len(values), limit))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value.Title = strings.TrimSpace(value.Title)
		value.Snippet = strings.TrimSpace(value.Snippet)
		if len([]rune(value.Snippet)) > 2000 {
			value.Snippet = string([]rune(value.Snippet)[:1999]) + "…"
		}
		parsed, err := url.Parse(strings.TrimSpace(value.URL))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			continue
		}
		parsed.Fragment = ""
		value.URL = parsed.String()
		if _, exists := seen[value.URL]; exists {
			continue
		}
		seen[value.URL] = struct{}{}
		if value.Title == "" {
			value.Title = value.URL
		}
		results = append(results, value)
		if len(results) >= limit {
			break
		}
	}
	return results
}

func StableSort(values []Result) {
	sort.SliceStable(values, func(left, right int) bool { return values[left].URL < values[right].URL })
}
