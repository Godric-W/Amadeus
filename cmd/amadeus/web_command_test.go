package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type commandWebFetcher struct {
	url      string
	document webfetch.Document
}

func (fetcher *commandWebFetcher) Fetch(_ context.Context, rawURL string) (webfetch.Document, error) {
	fetcher.url = rawURL
	return fetcher.document, nil
}

type commandWebSearch struct {
	query   string
	limit   int
	results []websearch.Result
}

func (search *commandWebSearch) Search(_ context.Context, query string, limit int) ([]websearch.Result, error) {
	search.query = query
	search.limit = limit
	return append([]websearch.Result(nil), search.results...), nil
}

func TestWebCheckRejectsDisabledCapabilities(t *testing.T) {
	command, _ := newTestRootCommand(t.TempDir())
	command.SetArgs([]string{"web", "check"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("unexpected disabled Web check error: %v", err)
	}
}

func TestWebCheckUsesConfiguredInjectedCapabilitiesWithoutLeakingKey(t *testing.T) {
	amadeusRoot := t.TempDir()
	const secret = "brave-secret-must-not-leak"
	writeCommandConfig(t, filepath.Join(amadeusRoot, "config.yaml"), `
web:
  fetch:
    enabled: true
  search:
    enabled: true
    provider: brave
    api_key: brave-secret-must-not-leak
`)
	fetcher := &commandWebFetcher{document: webfetch.Document{URL: "https://example.test/docs", ContentType: "text/plain", Markdown: "document"}}
	search := &commandWebSearch{results: []websearch.Result{{Title: "One", URL: "https://example.test", Snippet: "result"}}}
	var output bytes.Buffer
	command := newRootCommandWithRuntime(&configFlags{}, commandRuntime{
		amadeusRoot: amadeusRoot,
		lookupEnv:   emptyEnvLookup,
		webFetcher:  fetcher,
		webSearch:   search,
	})
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"web", "check", "--query", "amadeus", "--url", "https://example.test/docs"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run Web check: %v", err)
	}
	if fetcher.url != "https://example.test/docs" || search.query != "amadeus" || search.limit != 1 {
		t.Fatalf("unexpected Web checks: fetch=%q search=%q limit=%d", fetcher.url, search.query, search.limit)
	}
	if !strings.Contains(output.String(), "web fetch check: ok") || !strings.Contains(output.String(), "provider=brave") {
		t.Fatalf("unexpected Web check output: %q", output.String())
	}
	if strings.Contains(output.String(), secret) {
		t.Fatalf("Web check leaked API key: %q", output.String())
	}
}
