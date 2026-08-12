package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/policy"
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

type webApprovalStub struct {
	decision policy.ApprovalDecision
	requests []policy.ApprovalRequest
}

func (stub *webApprovalStub) Decide(_ context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	stub.requests = append(stub.requests, request.Clone())
	return stub.decision, nil
}

type countingWebFetcher struct {
	count    int
	document webfetch.Document
}

func (fetcher *countingWebFetcher) Fetch(_ context.Context, _ string) (webfetch.Document, error) {
	fetcher.count++
	return fetcher.document, nil
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

func TestWebFetchApprovalDenialPreventsFetch(t *testing.T) {
	fetcher := &countingWebFetcher{document: webfetch.Document{URL: "https://example.com", Text: "body"}}
	approvals := &webApprovalStub{decision: policy.ApprovalDecision{Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "not now"}}
	fetch, err := NewWebFetchWithApproval(fetcher, WebApprovalOptions{Approvals: approvals})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executePreparedTool(t, context.Background(), fetch, json.RawMessage(`{"url":"https://example.com/page"}`)); err == nil {
		t.Fatal("denied web fetch succeeded")
	}
	if fetcher.count != 0 || len(approvals.requests) != 1 {
		t.Fatalf("denied fetch reached provider or skipped approval: count=%d requests=%d", fetcher.count, len(approvals.requests))
	}
}

func TestWebFetchSessionApprovalIsScopedByHostname(t *testing.T) {
	fetcher := &countingWebFetcher{document: webfetch.Document{URL: "https://example.com", Text: "body"}}
	approvals := &webApprovalStub{decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalSession, Source: policy.ApprovalSourceUser, Reason: "trusted"}}
	events := event.NewMemorySink()
	fetch, err := NewWebFetchWithApproval(fetcher, WebApprovalOptions{Approvals: approvals, Events: events})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"url":"https://example.com/one"}`, `{"url":"https://example.com/two"}`, `{"url":"https://other.example/two"}`} {
		if _, err := executePreparedTool(t, context.Background(), fetch, json.RawMessage(raw)); err != nil {
			t.Fatalf("web fetch %s failed: %v", raw, err)
		}
	}
	if len(approvals.requests) != 2 {
		t.Fatalf("expected one approval per hostname, got %d", len(approvals.requests))
	}
	if fetcher.count != 3 {
		t.Fatalf("expected three fetches, got %d", fetcher.count)
	}
	if events.Len() != 4 {
		t.Fatalf("expected requested/resolved for two hosts, got %d events", events.Len())
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
