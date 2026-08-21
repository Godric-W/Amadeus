package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

const (
	ProviderDuckDuckGo = "duckduckgo"
	ProviderTavily     = "tavily"
	ProviderSearXNG    = "searxng"
	ProviderBrave      = "brave"

	defaultUserAgent       = "Amadeus/1.0"
	duckDuckGoUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	tavilySearchDepthBasic = "basic"
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

type duckDuckGo struct {
	client       *http.Client
	htmlEndpoint string
	apiEndpoint  string
}

func newDuckDuckGo(client *http.Client, baseURL string) (*duckDuckGo, error) {
	htmlEndpoint := "https://html.duckduckgo.com/html/"
	apiEndpoint := "https://api.duckduckgo.com/"
	if strings.TrimSpace(baseURL) != "" {
		parsed, err := validateEndpoint(baseURL)
		if err != nil {
			return nil, &Error{Kind: ErrorInvalidConfig, Provider: ProviderDuckDuckGo, Err: errors.New("invalid base URL")}
		}
		htmlEndpoint = parsed.ResolveReference(&url.URL{Path: "/html/"}).String()
		apiEndpoint = parsed.ResolveReference(&url.URL{Path: "/"}).String()
	}
	return &duckDuckGo{client: client, htmlEndpoint: htmlEndpoint, apiEndpoint: apiEndpoint}, nil
}

func (provider *duckDuckGo) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	values, htmlErr := provider.searchHTML(ctx, query, limit)
	if htmlErr == nil && len(values) > 0 {
		return values, nil
	}
	values, apiErr := provider.searchAPI(ctx, query, limit)
	if apiErr != nil {
		if htmlErr != nil {
			return nil, errors.Join(htmlErr, apiErr)
		}
		return nil, apiErr
	}
	return values, nil
}

func (provider *duckDuckGo) searchHTML(ctx context.Context, query string, limit int) ([]Result, error) {
	endpoint, err := url.Parse(provider.htmlEndpoint)
	if err != nil {
		return nil, &Error{Kind: ErrorProtocol, Provider: ProviderDuckDuckGo, Err: err}
	}
	parameters := endpoint.Query()
	parameters.Set("q", query)
	endpoint.RawQuery = parameters.Encode()
	request, err := newProviderRequest(ctx, ProviderDuckDuckGo, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("User-Agent", duckDuckGoUserAgent)
	body, err := execute(ctx, provider.client, ProviderDuckDuckGo, request)
	if err != nil {
		return nil, err
	}
	document, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Kind: ErrorProtocol, Provider: ProviderDuckDuckGo, Err: err}
	}
	results := make([]Result, 0, limit)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if len(results) >= limit {
			return
		}
		if node.Type == html.ElementNode && node.Data == "a" && hasClass(node, "result__a") {
			href := attribute(node, "href")
			results = append(results, Result{Title: nodeText(node), URL: decodeDuckDuckGoURL(href)})
		}
		if node.Type == html.ElementNode && hasClass(node, "result__snippet") && len(results) > 0 && results[len(results)-1].Snippet == "" {
			results[len(results)-1].Snippet = nodeText(node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return results, nil
}

func (provider *duckDuckGo) searchAPI(ctx context.Context, query string, limit int) ([]Result, error) {
	endpoint, err := url.Parse(provider.apiEndpoint)
	if err != nil {
		return nil, &Error{Kind: ErrorProtocol, Provider: ProviderDuckDuckGo, Err: err}
	}
	parameters := endpoint.Query()
	parameters.Set("q", query)
	parameters.Set("format", "json")
	parameters.Set("no_html", "1")
	parameters.Set("skip_disambig", "1")
	endpoint.RawQuery = parameters.Encode()
	request, err := newProviderRequest(ctx, ProviderDuckDuckGo, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", defaultUserAgent)
	body, err := execute(ctx, provider.client, ProviderDuckDuckGo, request)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Heading       string            `json:"Heading"`
		AbstractText  string            `json:"AbstractText"`
		AbstractURL   string            `json:"AbstractURL"`
		RelatedTopics []json.RawMessage `json:"RelatedTopics"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &Error{Kind: ErrorProtocol, Provider: ProviderDuckDuckGo, Err: err}
	}
	results := make([]Result, 0, limit)
	if payload.AbstractURL != "" {
		results = append(results, Result{Title: payload.Heading, URL: payload.AbstractURL, Snippet: payload.AbstractText})
	}
	for _, raw := range payload.RelatedTopics {
		collectDuckDuckGoResult(raw, &results, limit)
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func collectDuckDuckGoResult(raw json.RawMessage, results *[]Result, limit int) {
	if len(*results) >= limit {
		return
	}
	var item struct {
		Text     string            `json:"Text"`
		FirstURL string            `json:"FirstURL"`
		Topics   []json.RawMessage `json:"Topics"`
	}
	if json.Unmarshal(raw, &item) != nil {
		return
	}
	if item.FirstURL != "" {
		*results = append(*results, Result{Title: item.Text, URL: item.FirstURL, Snippet: item.Text})
		return
	}
	for _, nested := range item.Topics {
		collectDuckDuckGoResult(nested, results, limit)
	}
}

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
func hasClass(node *html.Node, class string) bool {
	for _, value := range strings.Fields(attribute(node, "class")) {
		if value == class {
			return true
		}
	}
	return false
}
func attribute(node *html.Node, name string) string {
	for _, value := range node.Attr {
		if value.Key == name {
			return value.Val
		}
	}
	return ""
}
func nodeText(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(value *html.Node) {
		if value.Type == html.TextNode {
			builder.WriteString(value.Data)
			builder.WriteByte(' ')
		}
		for child := value.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(builder.String()), " ")
}
func decodeDuckDuckGoURL(raw string) string {
	value, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	if target := value.Query().Get("uddg"); target != "" {
		return target
	}
	if value.IsAbs() {
		return value.String()
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}
