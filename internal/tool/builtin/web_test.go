package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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
	fetch, err := NewWebFetch(builtinWebFetcher{document: webfetch.Document{URL: "https://example.com", ContentType: "text/html", Title: "Example", Markdown: "# Body", Partial: true, Bytes: 128}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executePreparedTool(t, context.Background(), fetch, json.RawMessage(`{"url":"https://example.com"}`))
	if err != nil || !result.Partial || !strings.Contains(result.Text, "Untrusted web content") || !strings.Contains(result.Text, "# Body") || strings.Count(result.Text, "Example") != 1 || result.Metadata["title"] != "Example" || result.Metadata["evidence_type"] != "fetched_page" || fetch.Spec().SideEffect != tool.SideEffectNetwork {
		t.Fatalf("unexpected web fetch result: %#v err=%v", result, err)
	}
}

func TestWebFetchRejectsInvalidURLBeforeApproval(t *testing.T) {
	approvals := &webApprovalStub{decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "test"}}
	coordinator, err := policy.NewApprovalCoordinator(approvals)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := &countingWebFetcher{}
	fetch, err := NewWebFetch(fetcher)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withTestPermissions(withTestApprovalCoordinator(context.Background(), coordinator), policy.NewSessionPermissionContext())
	for _, raw := range []string{`{"url":"ftp://example.com/file"}`, `{"url":"https://user:secret@example.com/"}`, `{"url":"/relative"}`} {
		if _, err := executePreparedTool(t, ctx, fetch, json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid web_fetch input succeeded: %s", raw)
		}
	}
	if fetcher.count != 0 || len(approvals.requests) != 0 {
		t.Fatalf("invalid input reached approval or fetcher: fetches=%d approvals=%d", fetcher.count, len(approvals.requests))
	}
}

func TestWebFetchApprovalDenialPreventsFetch(t *testing.T) {
	fetcher := &countingWebFetcher{document: webfetch.Document{URL: "https://example.com", ContentType: "text/plain", Markdown: "body"}}
	approvals := &webApprovalStub{decision: policy.ApprovalDecision{Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "not now"}}
	fetch, err := NewWebFetch(fetcher)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := policy.NewApprovalCoordinator(approvals)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withTestPermissions(withTestApprovalCoordinator(context.Background(), coordinator), policy.NewSessionPermissionContext())
	if _, err := executePreparedTool(t, ctx, fetch, json.RawMessage(`{"url":"https://example.com/page"}`)); err == nil {
		t.Fatal("denied web fetch succeeded")
	}
	if fetcher.count != 0 || len(approvals.requests) != 1 {
		t.Fatalf("denied fetch reached provider or skipped approval: count=%d requests=%d", fetcher.count, len(approvals.requests))
	}
}

func TestWebFetchSessionApprovalIsScopedByHostname(t *testing.T) {
	fetcher := &countingWebFetcher{document: webfetch.Document{URL: "https://example.com", ContentType: "text/plain", Markdown: "body"}}
	approvals := &webApprovalStub{decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalSession, Source: policy.ApprovalSourceUser, Reason: "trusted"}}
	fetch, err := NewWebFetch(fetcher)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := policy.NewApprovalCoordinator(approvals)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withTestPermissions(withTestApprovalCoordinator(context.Background(), coordinator), policy.NewSessionPermissionContext())
	for _, raw := range []string{`{"url":"https://example.com/one"}`, `{"url":"https://example.com/two"}`, `{"url":"https://other.example/two"}`} {
		if _, err := executePreparedTool(t, ctx, fetch, json.RawMessage(raw)); err != nil {
			t.Fatalf("web fetch %s failed: %v", raw, err)
		}
	}
	if len(approvals.requests) != 2 {
		t.Fatalf("expected one approval per hostname, got %d", len(approvals.requests))
	}
	if fetcher.count != 3 {
		t.Fatalf("expected three fetches, got %d", fetcher.count)
	}
}

func TestWebFetchReturnsTypedCrossHostRedirectGuidance(t *testing.T) {
	redirectErr := &webfetch.Error{Kind: webfetch.ErrorRedirectApprovalNeeded, URL: "https://example.com/start", RedirectURL: "https://other.example/final", Err: errors.New("redirect approval required")}
	fetch, err := NewWebFetch(builtinWebFetcher{err: redirectErr})
	if err != nil {
		t.Fatal(err)
	}
	approvals := &webApprovalStub{decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "test"}}
	coordinator, err := policy.NewApprovalCoordinator(approvals)
	if err != nil {
		t.Fatal(err)
	}
	result, executeErr := executePreparedTool(t, withTestApprovalCoordinator(context.Background(), coordinator), fetch, json.RawMessage(`{"url":"https://example.com/start"}`))
	if executeErr == nil || result.Metadata["error_kind"] != string(webfetch.ErrorRedirectApprovalNeeded) || result.Metadata["redirect_url"] != "https://other.example/final" || !strings.Contains(result.Text, "Call web_fetch again") {
		t.Fatalf("unexpected redirect result: %#v err=%v", result, executeErr)
	}
}

func TestWebSearchFormatsProviderResultsAndErrors(t *testing.T) {
	search, err := NewWebSearch(builtinSearchProvider{results: []websearch.Result{{Title: "One", URL: "https://one.example", Snippet: "first"}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executePreparedTool(t, context.Background(), search, json.RawMessage(`{"query":"one","limit":1}`))
	if err != nil || !strings.Contains(result.Text, "https://one.example") || !strings.Contains(result.Text, "search-summary evidence") || result.Metadata["page_verified"] != false || search.Spec().SideEffect != tool.SideEffectNetwork {
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
