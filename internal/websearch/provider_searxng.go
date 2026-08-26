package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

type searXNG struct {
	client   *http.Client
	endpoint string
}

func newSearXNG(client *http.Client, endpoint string) (*searXNG, error) {
	value, err := validateEndpoint(endpoint)
	if err != nil {
		return nil, &Error{Kind: ErrorInvalidConfig, Provider: ProviderSearXNG, Err: err}
	}
	value.Path = strings.TrimRight(value.Path, "/")
	if !strings.HasSuffix(value.Path, "/search") {
		value.Path += "/search"
	}
	return &searXNG{client: client, endpoint: value.String()}, nil
}

func (provider *searXNG) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	endpoint, err := url.Parse(provider.endpoint)
	if err != nil {
		return nil, &Error{Kind: ErrorProtocol, Provider: ProviderSearXNG, Err: err}
	}
	values := endpoint.Query()
	values.Set("q", query)
	values.Set("format", "json")
	endpoint.RawQuery = values.Encode()
	request, err := newProviderRequest(ctx, ProviderSearXNG, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", defaultUserAgent)
	body, err := execute(ctx, provider.client, ProviderSearXNG, request)
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Results []struct{ Title, URL, Content string } `json:"results"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, &Error{Kind: ErrorProtocol, Provider: ProviderSearXNG, Err: err}
	}
	results := make([]Result, 0, min(limit, len(decoded.Results)))
	for _, item := range decoded.Results {
		results = append(results, Result{Title: item.Title, URL: item.URL, Snippet: item.Content})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}
