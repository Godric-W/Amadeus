package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type brave struct {
	client           *http.Client
	apiKey, endpoint string
}

func newBrave(client *http.Client, key, endpoint string) (*brave, error) {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = "https://api.search.brave.com/res/v1/web/search"
	}
	value, err := validateEndpoint(endpoint)
	if err != nil {
		return nil, &Error{Kind: ErrorInvalidConfig, Provider: ProviderBrave, Err: err}
	}
	return &brave{client: client, apiKey: strings.TrimSpace(key), endpoint: value.String()}, nil
}

func (provider *brave) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	endpoint, err := url.Parse(provider.endpoint)
	if err != nil {
		return nil, &Error{Kind: ErrorProtocol, Provider: ProviderBrave, Err: err}
	}
	values := endpoint.Query()
	values.Set("q", query)
	values.Set("count", fmt.Sprint(limit))
	endpoint.RawQuery = values.Encode()
	request, err := newProviderRequest(ctx, ProviderBrave, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", defaultUserAgent)
	request.Header.Set("X-Subscription-Token", provider.apiKey)
	body, err := execute(ctx, provider.client, ProviderBrave, request)
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Web struct {
			Results []struct{ Title, URL, Description string } `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, &Error{Kind: ErrorProtocol, Provider: ProviderBrave, Err: err}
	}
	results := make([]Result, 0, len(decoded.Web.Results))
	for _, item := range decoded.Web.Results {
		results = append(results, Result{Title: item.Title, URL: item.URL, Snippet: item.Description})
	}
	return results, nil
}
