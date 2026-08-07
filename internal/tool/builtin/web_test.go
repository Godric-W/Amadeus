package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type builtinWebFetcher struct {
	document webfetch.Document
	err      error
}

func (fetcher builtinWebFetcher) Fetch(context.Context, string) (webfetch.Document, error) {
	return fetcher.document, fetcher.err
}

type builtinSearchProvider struct {
	results []websearch.Result
	err     error
}

func (provider builtinSearchProvider) Search(context.Context, string, int) ([]websearch.Result, error) {
	return provider.results, provider.err
}

func TestWebFetchProducesUntrustedBoundedDocument(t *testing.T) {
	fetch, err := NewWebFetch(builtinWebFetcher{document: webfetch.Document{URL: "https://example.com", Title: "Example", Text: "body", Partial: true}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executePreparedTool(t, context.Background(), fetch, json.RawMessage(`{"url":"https://example.com"}`))
	if err != nil || !result.Partial || !strings.Contains(result.Text, "Untrusted web content") || result.Metadata["title"] != "Example" || fetch.Spec().SideEffect != tool.SideEffectNetwork {
		t.Fatalf("unexpected web fetch result: %#v err=%v", result, err)
	}
}

func TestWebSearchFormatsProviderResultsAndErrors(t *testing.T) {
	search, err := NewWebSearch(builtinSearchProvider{results: []websearch.Result{{Title: "One", URL: "https://one.example", Snippet: "first"}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executePreparedTool(t, context.Background(), search, json.RawMessage(`{"query":"one","limit":1}`))
	if err != nil || !strings.Contains(result.Text, "https://one.example") || search.Spec().SideEffect != tool.SideEffectNetwork {
		t.Fatalf("unexpected web search result: %#v err=%v", result, err)
	}
	failing, err := NewWebSearch(builtinSearchProvider{err: errors.New("offline")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executePreparedTool(t, context.Background(), failing, json.RawMessage(`{"query":"one"}`)); err == nil {
		t.Fatal("provider failure was not returned")
	}
}
