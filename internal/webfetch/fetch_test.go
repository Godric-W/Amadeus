package webfetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

var publicTestAddress = netip.MustParseAddr("93.184.216.34")

type fetchHarness struct {
	server *httptest.Server
	mutex  sync.Mutex
	dialed []string
}

func newFetchHarness(t *testing.T, handler http.Handler, options Options) (*HTTPFetcher, *fetchHarness) {
	t.Helper()
	harness := &fetchHarness{server: httptest.NewServer(handler)}
	t.Cleanup(harness.server.Close)
	if options.Resolve == nil {
		options.Resolve = func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{publicTestAddress}, nil
		}
	}
	options.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		harness.mutex.Lock()
		harness.dialed = append(harness.dialed, address)
		harness.mutex.Unlock()
		return (&net.Dialer{}).DialContext(ctx, network, harness.server.Listener.Addr().String())
	}
	options.Proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
	fetcher, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return fetcher, harness
}

func (harness *fetchHarness) dialAddresses() []string {
	harness.mutex.Lock()
	defer harness.mutex.Unlock()
	return append([]string(nil), harness.dialed...)
}

func TestFetchPinsValidatedAddressAndProjectsHTML(t *testing.T) {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Host != "public.example" {
			t.Fatalf("request Host = %q", request.Host)
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte(`<html><head><title>Example &amp; Test</title><style>ignore</style></head><body><nav>menu</nav><main><h1>Guide</h1><p>Hello <strong>world</strong>. Read <a href="/docs#part">docs</a>.</p><pre><code class="language-go">package main
func main() {}</code></pre></main><script>ignore</script></body></html>`))
	})
	fetcher, harness := newFetchHarness(t, handler, Options{MaxBytes: 4096, MaxRedirects: 3})
	document, err := fetcher.Fetch(context.Background(), "http://public.example/page#ignored")
	if err != nil {
		t.Fatal(err)
	}
	if document.URL != "http://public.example/page" || document.Title != "Example & Test" || document.ContentType != "text/html" || document.Partial {
		t.Fatalf("unexpected document metadata: %#v", document)
	}
	for _, expected := range []string{"# Guide", "Hello **world**", "[docs](http://public.example/docs)", "```go", "package main"} {
		if !strings.Contains(document.Markdown, expected) {
			t.Fatalf("markdown omitted %q: %q", expected, document.Markdown)
		}
	}
	if strings.Contains(document.Markdown, "Example & Test") || strings.Contains(document.Markdown, "menu") || strings.Contains(document.Markdown, "ignore") {
		t.Fatalf("markdown retained excluded or duplicate content: %q", document.Markdown)
	}
	addresses := harness.dialAddresses()
	if len(addresses) != 1 || addresses[0] != net.JoinHostPort(publicTestAddress.String(), "80") {
		t.Fatalf("dial addresses = %#v", addresses)
	}
}

func TestFetchReturnsPartialBoundedText(t *testing.T) {
	fetcher, _ := newFetchHarness(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = writer.Write([]byte("abcdefghij"))
	}), Options{MaxBytes: 5})
	document, err := fetcher.Fetch(context.Background(), "http://public.example/text")
	if err != nil {
		t.Fatal(err)
	}
	if !document.Partial || document.Markdown != "abcde" || document.Bytes != 5 {
		t.Fatalf("unexpected partial document: %#v", document)
	}
}

func TestFetchPinsValidatedIPv6Address(t *testing.T) {
	publicIPv6 := netip.MustParseAddr("2606:2800:220:1:248:1893:25c8:1946")
	fetcher, harness := newFetchHarness(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("ipv6"))
	}), Options{Resolve: func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{publicIPv6}, nil
	}})
	document, err := fetcher.Fetch(context.Background(), "http://public.example/ipv6")
	if err != nil || document.Markdown != "ipv6" {
		t.Fatalf("document=%#v err=%v", document, err)
	}
	addresses := harness.dialAddresses()
	if len(addresses) != 1 || addresses[0] != net.JoinHostPort(publicIPv6.String(), "80") {
		t.Fatalf("dial addresses = %#v", addresses)
	}
}

func TestFetchClassifiesHTTPStatusTimeoutAndCancel(t *testing.T) {
	t.Run("status", func(t *testing.T) {
		fetcher, _ := newFetchHarness(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusTooManyRequests)
		}), Options{})
		_, err := fetcher.Fetch(context.Background(), "http://public.example/status")
		assertFetchError(t, err, ErrorHTTPStatus)
		var fetchError *Error
		if !errors.As(err, &fetchError) || fetchError.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("unexpected status error: %#v", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		fetcher, _ := newFetchHarness(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_, _ = writer.Write([]byte("late"))
		}), Options{Timeout: 10 * time.Millisecond})
		_, err := fetcher.Fetch(context.Background(), "http://public.example/slow")
		assertFetchError(t, err, ErrorTimeout)
	})

	t.Run("cancel", func(t *testing.T) {
		fetcher, _ := newFetchHarness(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), Options{})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := fetcher.Fetch(ctx, "http://public.example/cancel")
		assertFetchError(t, err, ErrorCanceled)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error does not unwrap context.Canceled: %v", err)
		}
	})
}

func assertFetchError(t *testing.T, err error, kind ErrorKind) {
	t.Helper()
	var fetchError *Error
	if !errors.As(err, &fetchError) || fetchError.Kind != kind {
		t.Fatalf("error = %#v, want kind %q", err, kind)
	}
}
