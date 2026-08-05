package webfetch

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestFetchRejectsPrivateDestinationsAndExtractsBoundedHTML(t *testing.T) {
	privateResolve := func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
	}
	privateFetcher, err := New(Options{Resolve: privateResolve})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := privateFetcher.Fetch(context.Background(), "https://private.example/"); err == nil {
		t.Fatal("private destination was accepted")
	}

	publicResolve := func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(`<html><head><title>Example &amp; Test</title><style>ignore</style></head><body>Hello <b>world</b><script>ignore</script></body></html>`)), Request: request}, nil
	})}
	fetcher, err := New(Options{Resolve: publicResolve, HTTPClient: client, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	document, err := fetcher.Fetch(context.Background(), "https://public.example/path")
	if err != nil || document.Title != "Example & Test" || document.Text != "Example & Test Hello world" || document.URL != "https://public.example/path" {
		t.Fatalf("unexpected fetched document: %#v err=%v", document, err)
	}
}
