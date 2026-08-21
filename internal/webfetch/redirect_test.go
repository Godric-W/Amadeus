package webfetch

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFetchFollowsSameHostnameRedirectWithinLimit(t *testing.T) {
	var requests atomic.Int32
	fetcher, harness := newFetchHarness(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path == "/start" {
			http.Redirect(writer, request, "/final", http.StatusFound)
			return
		}
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("final content"))
	}), Options{MaxRedirects: 1})
	document, err := fetcher.Fetch(context.Background(), "http://public.example/start")
	if err != nil {
		t.Fatal(err)
	}
	if document.URL != "http://public.example/final" || document.Markdown != "final content" || requests.Load() != 2 || len(harness.dialAddresses()) != 2 {
		t.Fatalf("unexpected redirect result: document=%#v requests=%d dials=%#v", document, requests.Load(), harness.dialAddresses())
	}
}

func TestFetchRevalidatesDNSForSameHostnameRedirect(t *testing.T) {
	var resolves atomic.Int32
	fetcher, harness := newFetchHarness(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "/final", http.StatusFound)
	}), Options{
		MaxRedirects: 1,
		Resolve: func(context.Context, string) ([]netip.Addr, error) {
			if resolves.Add(1) == 1 {
				return []netip.Addr{publicTestAddress}, nil
			}
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
	})
	_, err := fetcher.Fetch(context.Background(), "http://public.example/start")
	assertFetchError(t, err, ErrorSSRFRejected)
	if resolves.Load() != 2 || len(harness.dialAddresses()) != 1 {
		t.Fatalf("redirect validation resolves=%d dials=%#v", resolves.Load(), harness.dialAddresses())
	}
}

func TestFetchRejectsRedirectBeyondConfiguredLimit(t *testing.T) {
	fetcher, _ := newFetchHarness(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/one":
			http.Redirect(writer, request, "/two", http.StatusFound)
		case "/two":
			http.Redirect(writer, request, "/three", http.StatusFound)
		default:
			writer.Header().Set("Content-Type", "text/plain")
			_, _ = writer.Write([]byte("unexpected"))
		}
	}), Options{MaxRedirects: 1})
	_, err := fetcher.Fetch(context.Background(), "http://public.example/one")
	assertFetchError(t, err, ErrorRedirectLimit)
}

func TestFetchStopsBeforeCrossHostnameRedirect(t *testing.T) {
	var requests atomic.Int32
	fetcher, _ := newFetchHarness(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		http.Redirect(writer, request, "http://other.example/final", http.StatusFound)
	}), Options{MaxRedirects: 3})
	_, err := fetcher.Fetch(context.Background(), "http://public.example/start")
	assertFetchError(t, err, ErrorRedirectApprovalNeeded)
	var fetchError *Error
	if !errors.As(err, &fetchError) || fetchError.RedirectURL != "http://other.example/final" || requests.Load() != 1 {
		t.Fatalf("unexpected cross-host redirect error: %#v requests=%d", fetchError, requests.Load())
	}
}

func TestFetchRejectsRedirectWhenLimitIsZero(t *testing.T) {
	fetcher, _ := newFetchHarness(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "/again", http.StatusFound)
	}), Options{MaxRedirects: 0})
	_, err := fetcher.Fetch(context.Background(), "http://public.example/start")
	assertFetchError(t, err, ErrorRedirectLimit)
	if !strings.Contains(err.Error(), "limit 0") {
		t.Fatalf("redirect error = %v", err)
	}
}
