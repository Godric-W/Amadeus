package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

const duckDuckGoUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

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
