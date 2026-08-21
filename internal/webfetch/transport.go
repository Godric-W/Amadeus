package webfetch

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/url"
)

type pinnedTransport struct {
	template *http.Transport
	resolve  Resolver
	dial     DialContext
	proxy    func(*http.Request) (*url.URL, error)
}

func newPinnedTransport(resolve Resolver, dial DialContext, proxy ProxyFunc, tlsConfig *tls.Config) *pinnedTransport {
	template := http.DefaultTransport.(*http.Transport).Clone()
	template.Proxy = nil
	template.DialContext = nil
	template.TLSClientConfig = cloneTLSConfig(tlsConfig)
	if proxy == nil {
		proxy = http.ProxyFromEnvironment
	}
	return &pinnedTransport{
		template: template,
		resolve:  resolve,
		dial:     dial,
		proxy:    proxy,
	}
}

func (transport *pinnedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, errorOf(ErrorInvalidURL, "", "web request URL is nil")
	}
	parsed, err := ParseURL(request.URL.String())
	if err != nil {
		return nil, err
	}
	target, err := resolveTarget(request.Context(), parsed, transport.resolve)
	if err != nil {
		return nil, err
	}

	proxyURL, err := transport.proxy(request)
	if err != nil {
		return nil, errorOf(ErrorNetwork, parsed.String(), "resolve web proxy: %v", err)
	}
	if proxyURL != nil {
		return transport.roundTripProxy(request, parsed, target, proxyURL)
	}
	current := transport.template.Clone()
	current.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return transport.dial(ctx, network, net.JoinHostPort(target.Address.String(), target.Port))
	}
	if parsed.Scheme == "https" {
		configuration := cloneTLSConfig(current.TLSClientConfig)
		configuration.ServerName = target.Hostname
		current.TLSClientConfig = configuration
	}

	pinnedURL := *parsed
	pinnedURL.Host = net.JoinHostPort(target.Address.String(), target.Port)
	pinnedRequest := request.Clone(request.Context())
	pinnedRequest.URL = &pinnedURL
	pinnedRequest.Host = parsed.Host

	response, err := current.RoundTrip(pinnedRequest)
	if err != nil {
		current.CloseIdleConnections()
		return nil, err
	}
	response.Request = request
	response.Body = &transportBody{ReadCloser: response.Body, closeIdle: current.CloseIdleConnections}
	return response, nil
}

func cloneTLSConfig(configuration *tls.Config) *tls.Config {
	if configuration == nil {
		return &tls.Config{MinVersion: tls.VersionTLS12}
	}
	cloned := configuration.Clone()
	if cloned.MinVersion == 0 {
		cloned.MinVersion = tls.VersionTLS12
	}
	return cloned
}

type transportBody struct {
	io.ReadCloser
	closeIdle func()
}

func (body *transportBody) Close() error {
	err := body.ReadCloser.Close()
	if body.closeIdle != nil {
		body.closeIdle()
	}
	return err
}
