package webfetch

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestParseURLRejectsUnsafeOrNonHTTPInputs(t *testing.T) {
	for _, rawURL := range []string{"", "/relative", "ftp://example.com/file", "https://user:secret@example.com/", "https://example.com:bad/", "https://example.com:70000/"} {
		t.Run(rawURL, func(t *testing.T) {
			if _, err := ParseURL(rawURL); err == nil {
				t.Fatalf("ParseURL(%q) succeeded", rawURL)
			}
		})
	}
	parsed, err := ParseURL(" HTTPS://Example.COM.:443/path#fragment ")
	if err != nil || parsed.String() != "https://example.com:443/path" {
		t.Fatalf("canonical URL = %v err=%v", parsed, err)
	}
	parsed, err = ParseURL("https://bücher.example/path")
	if err != nil || parsed.Hostname() != "xn--bcher-kva.example" {
		t.Fatalf("IDN URL = %v err=%v", parsed, err)
	}
}

func TestFetchRejectsRestrictedAndMixedDNSAnswersBeforeDial(t *testing.T) {
	for _, addresses := range [][]netip.Addr{
		{netip.MustParseAddr("10.0.0.1")},
		{publicTestAddress, netip.MustParseAddr("127.0.0.1")},
		{netip.MustParseAddr("fe80::1")},
	} {
		var dialed atomic.Bool
		fetcher, err := New(Options{
			Resolve: func(context.Context, string) ([]netip.Addr, error) { return addresses, nil },
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				dialed.Store(true)
				return nil, nil
			},
			Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		_, fetchErr := fetcher.Fetch(context.Background(), "http://public.example/")
		assertFetchError(t, fetchErr, ErrorSSRFRejected)
		if dialed.Load() {
			t.Fatalf("restricted DNS answer reached dial: %#v", addresses)
		}
	}
}
