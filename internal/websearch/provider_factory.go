package websearch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	ProviderDuckDuckGo = "duckduckgo"
	ProviderTavily     = "tavily"
	ProviderSearXNG    = "searxng"
	ProviderBrave      = "brave"

	defaultUserAgent = "Amadeus/1.0"
)

type ProviderOptions struct {
	Name       string
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

func NewProvider(options ProviderOptions) (Provider, error) {
	client := options.HTTPClient
	if client == nil {
		client = defaultHTTPClient()
	}
	switch strings.ToLower(strings.TrimSpace(options.Name)) {
	case ProviderDuckDuckGo:
		return newDuckDuckGo(client, options.BaseURL)
	case ProviderTavily:
		if strings.TrimSpace(options.APIKey) == "" {
			return nil, &Error{Kind: ErrorInvalidConfig, Provider: ProviderTavily, Err: errors.New("API key is empty")}
		}
		return newTavily(client, options.APIKey, options.BaseURL)
	case ProviderSearXNG:
		if strings.TrimSpace(options.BaseURL) == "" {
			return nil, &Error{Kind: ErrorInvalidConfig, Provider: ProviderSearXNG, Err: errors.New("base URL is empty")}
		}
		return newSearXNG(client, options.BaseURL)
	case ProviderBrave:
		if strings.TrimSpace(options.APIKey) == "" {
			return nil, &Error{Kind: ErrorInvalidConfig, Provider: ProviderBrave, Err: errors.New("API key is empty")}
		}
		return newBrave(client, options.APIKey, options.BaseURL)
	default:
		return nil, &Error{Kind: ErrorInvalidConfig, Provider: options.Name, Err: errors.New("unsupported provider")}
	}
}

func validateEndpoint(raw string) (*url.URL, error) {
	value, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !value.IsAbs() || value.Host == "" || (value.Scheme != "http" && value.Scheme != "https") || value.User != nil || value.Fragment != "" {
		return nil, errors.New("invalid base URL")
	}
	return value, nil
}

func newProviderRequest(ctx context.Context, provider, method, endpoint string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, &Error{Kind: ErrorProtocol, Provider: provider, Err: err}
	}
	return request, nil
}
