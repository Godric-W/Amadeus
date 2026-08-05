package webfetch

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxBytes     int64 = 1 << 20
	defaultMaxRedirects       = 3
	defaultTimeout            = 30 * time.Second
)

type Document struct {
	URL         string
	ContentType string
	Title       string
	Text        string
	Partial     bool
}

type Fetcher interface {
	Fetch(context.Context, string) (Document, error)
}

type Options struct {
	MaxBytes     int64
	MaxRedirects int
	Timeout      time.Duration
	MinInterval  time.Duration
	Resolve      func(context.Context, string) ([]netip.Addr, error)
	DialContext  func(context.Context, string, string) (net.Conn, error)
	HTTPClient   *http.Client
}

type HTTPFetcher struct {
	client      *http.Client
	maxBytes    int64
	checkURL    func(context.Context, *url.URL) error
	minInterval time.Duration
	mutex       sync.Mutex
	lastRequest time.Time
}

func New(options Options) (*HTTPFetcher, error) {
	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	maxRedirects := options.MaxRedirects
	if maxRedirects < 0 {
		return nil, errors.New("web fetch redirect limit cannot be negative")
	}
	if maxRedirects == 0 {
		maxRedirects = defaultMaxRedirects
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	resolve := options.Resolve
	if resolve == nil {
		resolve = resolveHost
	}
	checkURL := func(ctx context.Context, value *url.URL) error { return validateURL(ctx, value, resolve) }
	client := options.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyFromEnvironment
		dial := options.DialContext
		if dial == nil {
			dial = (&net.Dialer{}).DialContext
		}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if err := checkURL(ctx, &url.URL{Scheme: "http", Host: host}); err != nil {
				return nil, err
			}
			return dial(ctx, network, address)
		}
		client = &http.Client{Transport: transport, Timeout: timeout}
	} else {
		cloned := *client
		client = &cloned
		if client.Timeout == 0 {
			client.Timeout = timeout
		}
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return errors.New("web request exceeded redirect limit")
		}
		return checkURL(request.Context(), request.URL)
	}
	return &HTTPFetcher{client: client, maxBytes: maxBytes, checkURL: checkURL, minInterval: options.MinInterval}, nil
}

func (fetcher *HTTPFetcher) Fetch(ctx context.Context, rawURL string) (Document, error) {
	if fetcher == nil || fetcher.client == nil {
		return Document{}, errors.New("web fetcher is nil")
	}
	if err := fetcher.wait(ctx); err != nil {
		return Document{}, err
	}
	value, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return Document{}, fmt.Errorf("parse web URL: %w", err)
	}
	if err := fetcher.checkURL(ctx, value); err != nil {
		return Document{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, value.String(), nil)
	if err != nil {
		return Document{}, err
	}
	request.Header.Set("Accept", "text/html, text/plain;q=0.9, application/json;q=0.5")
	response, err := fetcher.client.Do(request)
	if err != nil {
		return Document{}, fmt.Errorf("fetch web URL: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Document{}, fmt.Errorf("fetch web URL returned HTTP %d", response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, fetcher.maxBytes+1))
	if err != nil {
		return Document{}, fmt.Errorf("read web response: %w", err)
	}
	partial := int64(len(content)) > fetcher.maxBytes
	if partial {
		content = content[:fetcher.maxBytes]
	}
	contentType := response.Header.Get("Content-Type")
	text, title := extractText(contentType, string(content))
	return Document{URL: response.Request.URL.String(), ContentType: contentType, Title: title, Text: text, Partial: partial}, nil
}

func (fetcher *HTTPFetcher) wait(ctx context.Context) error {
	if fetcher.minInterval <= 0 {
		return nil
	}
	fetcher.mutex.Lock()
	defer fetcher.mutex.Unlock()
	wait := fetcher.lastRequest.Add(fetcher.minInterval).Sub(time.Now())
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	fetcher.lastRequest = time.Now()
	return nil
}

func validateURL(ctx context.Context, value *url.URL, resolve func(context.Context, string) ([]netip.Addr, error)) error {
	if value == nil || (value.Scheme != "http" && value.Scheme != "https") || strings.TrimSpace(value.Hostname()) == "" || value.User != nil {
		return errors.New("web URL must be an absolute http or https URL without userinfo")
	}
	host := value.Hostname()
	if address, err := netip.ParseAddr(host); err == nil {
		if privateAddress(address) {
			return fmt.Errorf("web URL host %q resolves to a private address", host)
		}
		return nil
	}
	addresses, err := resolve(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve web URL host %q: %w", host, err)
	}
	if len(addresses) == 0 {
		return fmt.Errorf("resolve web URL host %q: no addresses", host)
	}
	for _, address := range addresses {
		if privateAddress(address) {
			return fmt.Errorf("web URL host %q resolves to a private address", host)
		}
	}
	return nil
}

func resolveHost(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func privateAddress(address netip.Addr) bool {
	address = address.Unmap()
	return !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified()
}

var (
	scriptStyle = regexp.MustCompile(`(?is)<(?:script|style)[^>]*>.*?</(?:script|style)\s*>`)
	titleTag    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title\s*>`)
	tags        = regexp.MustCompile(`(?s)<[^>]+>`)
	space       = regexp.MustCompile(`[\t\r\n ]+`)
)

func extractText(contentType, content string) (string, string) {
	if strings.Contains(strings.ToLower(contentType), "html") || strings.Contains(strings.ToLower(content), "<html") {
		title := ""
		if match := titleTag.FindStringSubmatch(content); len(match) == 2 {
			title = normalizeText(html.UnescapeString(tags.ReplaceAllString(match[1], " ")))
		}
		content = scriptStyle.ReplaceAllString(content, " ")
		return normalizeText(html.UnescapeString(tags.ReplaceAllString(content, " "))), title
	}
	return normalizeText(content), ""
}

func normalizeText(value string) string { return strings.TrimSpace(space.ReplaceAllString(value, " ")) }
