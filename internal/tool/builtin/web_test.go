package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/web"
)

type builtinWebFetcher struct {
	document web.Document
	err      error
}

func (fetcher builtinWebFetcher) Fetch(context.Context, string) (web.Document, error) {
	return fetcher.document, fetcher.err
}

type builtinSearchProvider struct {
	results []web.SearchResult
	err     error
}

func (provider builtinSearchProvider) Search(context.Context, string, int) ([]web.SearchResult, error) {
	return provider.results, provider.err
}

func TestWebFetchProducesUntrustedBoundedDocument(t *testing.T) {
	fetch, err := NewWebFetch(builtinWebFetcher{document: web.Document{URL: "https://example.com", Title: "Example", Text: "body", Partial: true}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fetch.Execute(context.Background(), json.RawMessage(`{"url":"https://example.com"}`))
	if err != nil || !result.Partial || !strings.Contains(result.Text, "Untrusted web content") || result.Metadata["title"] != "Example" || fetch.Spec().SideEffect != tool.SideEffectNetwork {
		t.Fatalf("unexpected web fetch result: %#v err=%v", result, err)
	}
}

func TestWebSearchFormatsProviderResultsAndErrors(t *testing.T) {
	search, err := NewWebSearch(builtinSearchProvider{results: []web.SearchResult{{Title: "One", URL: "https://one.example", Snippet: "first"}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := search.Execute(context.Background(), json.RawMessage(`{"query":"one","limit":1}`))
	if err != nil || !strings.Contains(result.Text, "https://one.example") || search.Spec().SideEffect != tool.SideEffectNetwork {
		t.Fatalf("unexpected web search result: %#v err=%v", result, err)
	}
	failing, err := NewWebSearch(builtinSearchProvider{err: errors.New("offline")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failing.Execute(context.Background(), json.RawMessage(`{"query":"one"}`)); err == nil {
		t.Fatal("provider failure was not returned")
	}
}
