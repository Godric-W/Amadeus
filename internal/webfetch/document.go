package webfetch

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

const (
	defaultMaxBytes int64 = 1 << 20
	defaultTimeout        = 30 * time.Second
)

type Document struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Title       string `json:"title,omitempty"`
	Markdown    string `json:"markdown"`
	Partial     bool   `json:"partial,omitempty"`
	Bytes       int64  `json:"bytes"`
}

type Fetcher interface {
	Fetch(context.Context, string) (Document, error)
}

type Resolver func(context.Context, string) ([]netip.Addr, error)
type DialContext func(context.Context, string, string) (net.Conn, error)
type ProxyFunc func(*http.Request) (*url.URL, error)

type Options struct {
	MaxBytes     int64
	MaxRedirects int
	Timeout      time.Duration
	MinInterval  time.Duration
	Resolve      Resolver
	DialContext  DialContext
	Proxy        ProxyFunc
	TLSConfig    *tls.Config
}
