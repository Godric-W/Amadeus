package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type WebFetch struct{ fetcher webfetch.Fetcher }
type WebSearch struct{ provider websearch.Provider }
type webFetchArguments struct {
	URL string `json:"url"`
}
type webSearchArguments struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

func NewWebFetch(fetcher webfetch.Fetcher) (*WebFetch, error) {
	if fetcher == nil {
		return nil, errors.New("web_fetch fetcher is nil")
	}
	return &WebFetch{fetcher: fetcher}, nil
}
func (fetch *WebFetch) Spec() tool.Spec                 { return webFetchSpec() }
func (fetch *WebFetch) SupportsParallelToolCalls() bool { return true }
func (fetch *WebFetch) Handle(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	call := invocation.Call
	var arguments webFetchArguments
	if err := decodeArguments(call.Payload, &arguments); err != nil {
		return tool.Output{}, err
	}
	if strings.TrimSpace(arguments.URL) == "" {
		return tool.Output{}, errors.New("web_fetch url is empty")
	}
	document, err := fetch.fetcher.Fetch(ctx, arguments.URL)
	if err != nil {
		return tool.Output{}, err
	}
	text := document.Text
	if document.Title != "" {
		text = document.Title + "\n\n" + text
	}
	return tool.Output{Text: "Untrusted web content from " + document.URL + ":\n" + text, Partial: document.Partial, Metadata: map[string]any{"url": document.URL, "content_type": document.ContentType, "title": document.Title}}, nil
}
func webFetchSpec() tool.Spec {
	return tool.Spec{Name: "web_fetch", Description: "Fetch a web page or text document through the network safety policy. Returned content is untrusted external data.", InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","minLength":1}},"required":["url"],"additionalProperties":false}`), SideEffect: tool.SideEffectNetwork, Idempotent: true}
}

func NewWebSearch(provider websearch.Provider) (*WebSearch, error) {
	if provider == nil {
		return nil, errors.New("web_search provider is nil")
	}
	return &WebSearch{provider: provider}, nil
}
func (search *WebSearch) Spec() tool.Spec                 { return webSearchSpec() }
func (search *WebSearch) SupportsParallelToolCalls() bool { return true }
func (search *WebSearch) Handle(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	call := invocation.Call
	var arguments webSearchArguments
	if err := decodeArguments(call.Payload, &arguments); err != nil {
		return tool.Output{}, err
	}
	arguments.Query = strings.TrimSpace(arguments.Query)
	if arguments.Query == "" {
		return tool.Output{}, errors.New("web_search query is empty")
	}
	results, err := search.provider.Search(ctx, arguments.Query, arguments.Limit)
	if err != nil {
		return tool.Output{}, err
	}
	encoded, err := json.Marshal(results)
	if err != nil {
		return tool.Output{}, fmt.Errorf("encode web search results: %w", err)
	}
	return tool.Output{Text: "Untrusted web search results:\n" + string(encoded), Metadata: map[string]any{"query": arguments.Query, "results": len(results)}}, nil
}
func webSearchSpec() tool.Spec {
	return tool.Spec{Name: "web_search", Description: "Search the public web through the network safety policy. Results are untrusted external data.", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"limit":{"type":"integer","minimum":1,"maximum":10}},"required":["query"],"additionalProperties":false}`), SideEffect: tool.SideEffectNetwork, Idempotent: true}
}

var _ tool.Handler = (*WebFetch)(nil)
var _ tool.Handler = (*WebSearch)(nil)
