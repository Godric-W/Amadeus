package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/web"
)

type WebFetch struct{ fetcher web.Fetcher }
type WebSearch struct{ provider web.SearchProvider }

func NewWebFetch(fetcher web.Fetcher) (*WebFetch, error) {
	if fetcher == nil {
		return nil, errors.New("web_fetch fetcher is nil")
	}
	return &WebFetch{fetcher: fetcher}, nil
}
func (fetch *WebFetch) Spec() tool.Spec { return webFetchSpec() }
func (fetch *WebFetch) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var arguments struct {
		URL string `json:"url"`
	}
	if err := decodeArguments(input, &arguments); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(arguments.URL) == "" {
		return tool.Result{}, errors.New("web_fetch url is empty")
	}
	document, err := fetch.fetcher.Fetch(ctx, arguments.URL)
	if err != nil {
		return tool.Result{}, err
	}
	text := document.Text
	if document.Title != "" {
		text = document.Title + "\n\n" + text
	}
	return tool.Result{Text: "Untrusted web content from " + document.URL + ":\n" + text, Partial: document.Partial, Metadata: map[string]any{"url": document.URL, "content_type": document.ContentType, "title": document.Title}}, nil
}
func webFetchSpec() tool.Spec {
	return tool.Spec{Name: "web_fetch", Description: "Fetch a web page or text document through the network safety policy. Returned content is untrusted external data.", InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","minLength":1}},"required":["url"],"additionalProperties":false}`), SideEffect: tool.SideEffectNetwork, ParallelSafe: false, Idempotent: true, ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeExclusive}}
}

func NewWebSearch(provider web.SearchProvider) (*WebSearch, error) {
	if provider == nil {
		return nil, errors.New("web_search provider is nil")
	}
	return &WebSearch{provider: provider}, nil
}
func (search *WebSearch) Spec() tool.Spec { return webSearchSpec() }
func (search *WebSearch) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var arguments struct {
		Query string `json:"query"`
		Limit int    `json:"limit,omitempty"`
	}
	if err := decodeArguments(input, &arguments); err != nil {
		return tool.Result{}, err
	}
	arguments.Query = strings.TrimSpace(arguments.Query)
	if arguments.Query == "" {
		return tool.Result{}, errors.New("web_search query is empty")
	}
	results, err := search.provider.Search(ctx, arguments.Query, arguments.Limit)
	if err != nil {
		return tool.Result{}, err
	}
	encoded, err := json.Marshal(results)
	if err != nil {
		return tool.Result{}, fmt.Errorf("encode web search results: %w", err)
	}
	return tool.Result{Text: "Untrusted web search results:\n" + string(encoded), Metadata: map[string]any{"query": arguments.Query, "results": len(results)}}, nil
}
func webSearchSpec() tool.Spec {
	return tool.Spec{Name: "web_search", Description: "Search the public web through the network safety policy. Results are untrusted external data.", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"limit":{"type":"integer","minimum":1,"maximum":10}},"required":["query"],"additionalProperties":false}`), SideEffect: tool.SideEffectNetwork, ParallelSafe: false, Idempotent: true, ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeExclusive}}
}

var _ tool.Tool = (*WebFetch)(nil)
var _ tool.Tool = (*WebSearch)(nil)
