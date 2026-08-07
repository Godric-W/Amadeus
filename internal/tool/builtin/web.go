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
func (fetch *WebFetch) Spec() tool.Spec { return webFetchSpec() }
func (fetch *WebFetch) Prepare(ctx context.Context, call tool.Call) (tool.PreparedCall, error) {
	var arguments webFetchArguments
	if err := decodeArguments(call.Arguments, &arguments); err != nil {
		return tool.PreparedCall{}, err
	}
	if strings.TrimSpace(arguments.URL) == "" {
		return tool.PreparedCall{}, errors.New("web_fetch url is empty")
	}
	return tool.NewPreparedCall(call, tool.PreparedOptions{Targets: []tool.PreparedTarget{{Kind: tool.TargetWeb, Access: tool.TargetAccessNetwork, Identity: arguments.URL}}, Payload: arguments})
}
func (fetch *WebFetch) Execute(ctx context.Context, prepared tool.PreparedCall) (tool.Result, error) {
	arguments, err := preparedPayload[webFetchArguments](prepared, "web_fetch")
	if err != nil {
		return tool.Result{}, err
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
	return tool.Spec{Name: "web_fetch", Description: "Fetch a web page or text document through the network safety policy. Returned content is untrusted external data.", InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","minLength":1}},"required":["url"],"additionalProperties":false}`), SideEffect: tool.SideEffectNetwork, Concurrency: tool.ToolConcurrencyShared, Idempotent: true}
}

func NewWebSearch(provider websearch.Provider) (*WebSearch, error) {
	if provider == nil {
		return nil, errors.New("web_search provider is nil")
	}
	return &WebSearch{provider: provider}, nil
}
func (search *WebSearch) Spec() tool.Spec { return webSearchSpec() }
func (search *WebSearch) Prepare(ctx context.Context, call tool.Call) (tool.PreparedCall, error) {
	var arguments webSearchArguments
	if err := decodeArguments(call.Arguments, &arguments); err != nil {
		return tool.PreparedCall{}, err
	}
	arguments.Query = strings.TrimSpace(arguments.Query)
	if arguments.Query == "" {
		return tool.PreparedCall{}, errors.New("web_search query is empty")
	}
	return tool.NewPreparedCall(call, tool.PreparedOptions{Targets: []tool.PreparedTarget{{Kind: tool.TargetWeb, Access: tool.TargetAccessNetwork, Identity: arguments.Query}}, Payload: arguments})
}
func (search *WebSearch) Execute(ctx context.Context, prepared tool.PreparedCall) (tool.Result, error) {
	arguments, err := preparedPayload[webSearchArguments](prepared, "web_search")
	if err != nil {
		return tool.Result{}, err
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
	return tool.Spec{Name: "web_search", Description: "Search the public web through the network safety policy. Results are untrusted external data.", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"limit":{"type":"integer","minimum":1,"maximum":10}},"required":["query"],"additionalProperties":false}`), SideEffect: tool.SideEffectNetwork, Concurrency: tool.ToolConcurrencyShared, Idempotent: true}
}

var _ tool.Tool = (*WebFetch)(nil)
var _ tool.Tool = (*WebSearch)(nil)
