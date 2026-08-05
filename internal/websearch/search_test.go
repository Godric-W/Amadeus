package websearch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func response(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func TestDuckDuckGoUsesHTMLFirstAndAPIFallback(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Path == "/html/" {
			return response(request, http.StatusOK, `<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fone.example%2F">One</a><div class="result__snippet">First</div>`), nil
		}
		return response(request, http.StatusOK, `{"Heading":"Fallback","AbstractText":"Second","AbstractURL":"https://two.example/"}`), nil
	})}
	provider, err := NewProvider(ProviderOptions{Name: ProviderDuckDuckGo, BaseURL: "https://search.example", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	results, err := provider.Search(context.Background(), "query", 3)
	if err != nil || requests != 1 || len(results) != 1 || results[0].URL != "https://one.example/" || results[0].Snippet != "First" {
		t.Fatalf("unexpected HTML search: requests=%d results=%#v err=%v", requests, results, err)
	}

	requests = 0
	client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Path == "/html/" {
			return response(request, http.StatusOK, `<html>no results</html>`), nil
		}
		return response(request, http.StatusOK, `{"Heading":"Fallback","AbstractText":"Second","AbstractURL":"https://two.example/"}`), nil
	})
	results, err = provider.Search(context.Background(), "query", 3)
	if err != nil || requests != 2 || len(results) != 1 || results[0].URL != "https://two.example/" {
		t.Fatalf("unexpected API fallback: requests=%d results=%#v err=%v", requests, results, err)
	}
}

func TestJSONProvidersSerializeAuthenticationAndNormalizeResults(t *testing.T) {
	tests := []struct {
		name    string
		options ProviderOptions
		assert  func(*testing.T, *http.Request, string)
		body    string
	}{
		{name: ProviderTavily, options: ProviderOptions{Name: ProviderTavily, APIKey: "tavily-key", BaseURL: "https://search.example/tavily"}, body: `{"results":[{"title":"One","url":"https://one.example/#fragment","content":"First"}]}`, assert: func(t *testing.T, request *http.Request, body string) {
			if request.Method != http.MethodPost || !strings.Contains(body, `"api_key":"tavily-key"`) {
				t.Fatalf("unexpected Tavily request: %s %s", request.Method, body)
			}
		}},
		{name: ProviderSearXNG, options: ProviderOptions{Name: ProviderSearXNG, BaseURL: "https://search.example"}, body: `{"results":[{"title":"One","url":"https://one.example/","content":"First"}]}`, assert: func(t *testing.T, request *http.Request, _ string) {
			if request.URL.Path != "/search" || request.URL.Query().Get("format") != "json" {
				t.Fatalf("unexpected SearXNG request: %s", request.URL)
			}
		}},
		{name: ProviderBrave, options: ProviderOptions{Name: ProviderBrave, APIKey: "brave-key", BaseURL: "https://search.example/brave"}, body: `{"web":{"results":[{"title":"One","url":"https://one.example/","description":"First"}]}}`, assert: func(t *testing.T, request *http.Request, _ string) {
			if request.Header.Get("X-Subscription-Token") != "brave-key" || request.URL.Query().Get("count") != "2" {
				t.Fatalf("unexpected Brave request: %#v %s", request.Header, request.URL)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var content []byte
				if request.Body != nil {
					content, _ = io.ReadAll(request.Body)
				}
				test.assert(t, request, string(content))
				return response(request, http.StatusOK, test.body), nil
			})}
			test.options.HTTPClient = client
			provider, err := NewProvider(test.options)
			if err != nil {
				t.Fatal(err)
			}
			service, _ := NewService(provider, ServiceOptions{Timeout: time.Second, MaxResults: 2, RetryDelay: time.Millisecond})
			results, err := service.Search(context.Background(), "query", 2)
			if err != nil || len(results) != 1 || results[0].URL != "https://one.example/" {
				t.Fatalf("unexpected results: %#v err=%v", results, err)
			}
		})
	}
}

type sequenceProvider struct {
	mutex   sync.Mutex
	calls   int
	results []Result
	errors  []error
}

func (provider *sequenceProvider) Search(context.Context, string, int) ([]Result, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.calls++
	if len(provider.errors) > 0 {
		err := provider.errors[0]
		provider.errors = provider.errors[1:]
		return nil, err
	}
	return append([]Result(nil), provider.results...), nil
}

func TestServiceRetriesTransientClassifiesTimeoutAndDeduplicates(t *testing.T) {
	provider := &sequenceProvider{errors: []error{&Error{Kind: ErrorUpstream, Provider: "fixture", StatusCode: 503, Err: errors.New("down")}}, results: []Result{{Title: "One", URL: "https://one.example/#a"}, {Title: "Duplicate", URL: "https://one.example/#b"}, {Title: "Invalid", URL: "file:///tmp/a"}}}
	service, _ := NewService(provider, ServiceOptions{Timeout: time.Second, MaxResults: 5, RetryDelay: time.Millisecond})
	results, err := service.Search(context.Background(), "query", 5)
	if err != nil || provider.calls != 2 || len(results) != 1 || results[0].URL != "https://one.example/" {
		t.Fatalf("unexpected retry/dedup: calls=%d results=%#v err=%v", provider.calls, results, err)
	}

	timeoutProvider := ProviderFunc(func(ctx context.Context, _ string, _ int) ([]Result, error) { <-ctx.Done(); return nil, ctx.Err() })
	timeoutService, _ := NewService(timeoutProvider, ServiceOptions{Timeout: 5 * time.Millisecond, MaxResults: 1, RetryDelay: time.Millisecond})
	_, err = timeoutService.Search(context.Background(), "query", 1)
	var searchErr *Error
	if !errors.As(err, &searchErr) || searchErr.Kind != ErrorTimeout {
		t.Fatalf("unexpected timeout error: %v", err)
	}
}

func TestHTTPProvidersClassifyStatusesAndAcceptEmptyResults(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		kind   ErrorKind
	}{
		{name: "authentication", status: http.StatusUnauthorized, kind: ErrorAuthentication},
		{name: "rate limit", status: http.StatusTooManyRequests, kind: ErrorRateLimit},
		{name: "upstream", status: http.StatusServiceUnavailable, kind: ErrorUpstream},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return response(request, test.status, `{"error":"provider failure"}`), nil
			})}
			provider, err := NewProvider(ProviderOptions{Name: ProviderBrave, APIKey: "secret", BaseURL: "https://search.example/brave", HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Search(context.Background(), "query", 1)
			var searchErr *Error
			if !errors.As(err, &searchErr) || searchErr.Kind != test.kind || searchErr.StatusCode != test.status {
				t.Fatalf("unexpected provider error: %#v", err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("provider error leaked API key: %v", err)
			}
		})
	}

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return response(request, http.StatusOK, `{"web":{"results":[]}}`), nil
	})}
	provider, err := NewProvider(ProviderOptions{Name: ProviderBrave, APIKey: "secret", BaseURL: "https://search.example/brave", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	results, err := provider.Search(context.Background(), "query", 1)
	if err != nil || len(results) != 0 {
		t.Fatalf("empty results should succeed: results=%#v err=%v", results, err)
	}
}

func TestProviderRejectsIncompleteConfiguration(t *testing.T) {
	for _, options := range []ProviderOptions{
		{Name: ProviderTavily},
		{Name: ProviderBrave},
		{Name: ProviderSearXNG},
		{Name: "unknown"},
	} {
		_, err := NewProvider(options)
		var searchErr *Error
		if !errors.As(err, &searchErr) || searchErr.Kind != ErrorInvalidConfig {
			t.Fatalf("options %#v returned %v", options, err)
		}
	}
}

type ProviderFunc func(context.Context, string, int) ([]Result, error)

func (function ProviderFunc) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	return function(ctx, query, limit)
}
