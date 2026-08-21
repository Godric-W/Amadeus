package webfetch

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync"
	"testing"
)

func TestPinnedTransportPreservesTLSServerName(t *testing.T) {
	var mutex sync.Mutex
	serverName := ""
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("secure"))
	}))
	server.TLS = &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			mutex.Lock()
			serverName = hello.ServerName
			mutex.Unlock()
			return nil, nil
		},
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	fetcher, err := New(Options{
		Resolve: func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{publicTestAddress}, nil },
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
		Proxy:     func(*http.Request) (*url.URL, error) { return nil, nil },
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, // test server certificate is issued for loopback
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := fetcher.Fetch(context.Background(), "https://public.example/secure")
	if err != nil || document.Markdown != "secure" {
		t.Fatalf("document=%#v err=%v", document, err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if serverName != "public.example" {
		t.Fatalf("TLS SNI = %q", serverName)
	}
}

func TestPinnedTransportUsesProxyWithoutProxyDNSForTarget(t *testing.T) {
	var gotConnectHost, gotHost string
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotConnectHost = request.Host
		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			t.Fatal("proxy response writer cannot hijack")
		}
		connection, buffered, err := hijacker.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffered.Flush()
		tunneled, err := http.ReadRequest(bufio.NewReader(connection))
		if err != nil {
			t.Fatal(err)
		}
		gotHost = tunneled.Host
		_, _ = connection.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 7\r\nConnection: close\r\n\r\nproxied"))
	}))
	t.Cleanup(proxy.Close)
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	fetcher, err := New(Options{
		Resolve: func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{publicTestAddress}, nil },
		Proxy:   func(*http.Request) (*url.URL, error) { return proxyURL, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := fetcher.Fetch(context.Background(), "http://public.example/proxied")
	if err != nil || document.Markdown != "proxied" {
		t.Fatalf("document=%#v err=%v", document, err)
	}
	if gotConnectHost != net.JoinHostPort(publicTestAddress.String(), "80") || gotHost != "public.example" {
		t.Fatalf("proxy CONNECT host=%q tunneled Host=%q", gotConnectHost, gotHost)
	}
}
