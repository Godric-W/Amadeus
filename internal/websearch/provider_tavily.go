package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

const tavilySearchDepthBasic = "basic"

type tavily struct {
	client           *http.Client
	apiKey, endpoint string
}

func newTavily(client *http.Client, key, endpoint string) (*tavily, error) {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = "https://api.tavily.com/search"
	}
	value, err := validateEndpoint(endpoint)
	if err != nil {
		return nil, &Error{Kind: ErrorInvalidConfig, Provider: ProviderTavily, Err: err}
	}
	return &tavily{client: client, apiKey: strings.TrimSpace(key), endpoint: value.String()}, nil
}

func (provider *tavily) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	payload, err := json.Marshal(struct {
		Query       string `json:"query"`
		MaxResults  int    `json:"max_results"`
		SearchDepth string `json:"search_depth"`
	}{Query: query, MaxResults: limit, SearchDepth: tavilySearchDepthBasic})
	if err != nil {
		return nil, &Error{Kind: ErrorProtocol, Provider: ProviderTavily, Err: err}
	}
	request, err := newProviderRequest(ctx, ProviderTavily, http.MethodPost, provider.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+provider.apiKey)
	request.Header.Set("Content-Type", "application/json")
	body, err := execute(ctx, provider.client, ProviderTavily, request)
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Results []struct{ Title, URL, Content string } `json:"results"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, &Error{Kind: ErrorProtocol, Provider: ProviderTavily, Err: err}
	}
	results := make([]Result, 0, len(decoded.Results))
	for _, item := range decoded.Results {
		results = append(results, Result{Title: item.Title, URL: item.URL, Snippet: item.Content})
	}
	return results, nil
}
