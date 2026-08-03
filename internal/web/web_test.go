package web

import (
	"context"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fixtureFetcher struct {
	document Document
	url      string
}

func (fetcher *fixtureFetcher) Fetch(_ context.Context, url string) (Document, error) {
	fetcher.url = url
	return fetcher.document, nil
}

func TestURLPolicyRejectsPrivateAndUnsupportedDestinations(t *testing.T) {
	resolve := func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
	}
	for _, raw := range []string{"file:///etc/passwd", "https://127.0.0.1/", "https://private.example/"} {
		value, _ := parseURL(raw)
		if err := validateURL(context.Background(), value, resolve); err == nil {
			t.Fatalf("unsafe URL was accepted: %s", raw)
		}
	}
	value, _ := parseURL("https://public.example/path")
	resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	if err := validateURL(context.Background(), value, resolve); err != nil {
		t.Fatalf("public URL was rejected: %v", err)
	}
}

func TestExtractTextRemovesMarkupAndPreservesStableTitle(t *testing.T) {
	text, title := extractText("text/html", `<html><head><title>Example &amp; Test</title><style>ignore</style></head><body>Hello <b>world</b><script>alert(1)</script></body></html>`)
	if title != "Example & Test" || text != "Example & Test Hello world" {
		t.Fatalf("unexpected HTML extraction: title=%q text=%q", title, text)
	}
}

func TestDuckDuckGoProviderFormatsMockResponse(t *testing.T) {
	fetcher := &fixtureFetcher{document: Document{Text: `{"Heading":"One","AbstractText":"First","AbstractURL":"https://one.example","RelatedTopics":[{"Text":"Two","FirstURL":"https://two.example"}]}`}}
	provider, err := NewDuckDuckGoProviderWithEndpoint(fetcher, "https://search.example/")
	if err != nil {
		t.Fatal(err)
	}
	results, err := provider.Search(context.Background(), "query words", 2)
	if err != nil || len(results) != 2 || results[0].URL != "https://one.example" || results[1].URL != "https://two.example" || !strings.Contains(fetcher.url, "q=query+words") {
		t.Fatalf("unexpected mock search result: results=%#v url=%q err=%v", results, fetcher.url, err)
	}
}

func TestFetcherRateLimitHonorsCancellation(t *testing.T) {
	fetcher := &HTTPFetcher{minInterval: time.Second, lastRequest: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fetcher.wait(ctx); err != context.Canceled {
		t.Fatalf("rate-limit wait did not honor cancellation: %v", err)
	}
}

func parseURL(raw string) (*url.URL, error) { return url.Parse(raw) }
