package webfetch

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func (transport *pinnedTransport) roundTripProxy(request *http.Request, parsed *url.URL, target resolvedTarget, proxyURL *url.URL) (*http.Response, error) {
	proxyScheme := strings.ToLower(proxyURL.Scheme)
	if proxyScheme != "http" && proxyScheme != "https" {
		return nil, errorOf(ErrorNetwork, parsed.String(), "web proxy must use http or https")
	}
	proxyHost := proxyURL.Hostname()
	if proxyHost == "" {
		return nil, errorOf(ErrorNetwork, parsed.String(), "web proxy has no hostname")
	}
	proxyPort := proxyURL.Port()
	if proxyPort == "" {
		if proxyScheme == "https" {
			proxyPort = "443"
		} else {
			proxyPort = "80"
		}
	}
	connection, err := transport.dial(request.Context(), "tcp", net.JoinHostPort(proxyHost, proxyPort))
	if err != nil {
		return nil, errorOf(ErrorNetwork, parsed.String(), "connect to web proxy: %v", err)
	}
	stopCancel := context.AfterFunc(request.Context(), func() { _ = connection.Close() })
	closeOnError := true
	defer func() {
		if closeOnError {
			stopCancel()
			_ = connection.Close()
		}
	}()
	if proxyScheme == "https" {
		configuration := cloneTLSConfig(transport.template.TLSClientConfig)
		configuration.ServerName = proxyHost
		secure := tls.Client(connection, configuration)
		if err := secure.HandshakeContext(request.Context()); err != nil {
			return nil, errorOf(ErrorNetwork, parsed.String(), "establish TLS with web proxy: %v", err)
		}
		connection = secure
	}

	reader := bufio.NewReader(connection)
	if err := establishProxyTunnel(request.Context(), connection, reader, target, proxyURL); err != nil {
		return nil, errorOf(ErrorNetwork, parsed.String(), "establish web proxy tunnel: %v", err)
	}
	if parsed.Scheme == "https" {
		configuration := cloneTLSConfig(transport.template.TLSClientConfig)
		configuration.ServerName = target.Hostname
		secure := tls.Client(connection, configuration)
		if err := secure.HandshakeContext(request.Context()); err != nil {
			return nil, errorOf(ErrorNetwork, parsed.String(), "establish TLS with web target: %v", err)
		}
		connection = secure
		reader = bufio.NewReader(connection)
	}
	if err := writeOriginRequest(connection, request); err != nil {
		return nil, errorOf(ErrorNetwork, parsed.String(), "write tunneled web request: %v", err)
	}

	response, err := http.ReadResponse(reader, request)
	if err != nil {
		return nil, errorOf(ErrorNetwork, parsed.String(), "read web proxy response: %v", err)
	}
	response.Request = request
	response.Body = &connectionBody{ReadCloser: response.Body, connection: connection, stopCancel: stopCancel}
	closeOnError = false
	return response, nil
}

func establishProxyTunnel(ctx context.Context, connection net.Conn, reader *bufio.Reader, target resolvedTarget, proxyURL *url.URL) error {
	address := net.JoinHostPort(target.Address.String(), target.Port)
	request, err := http.NewRequestWithContext(ctx, http.MethodConnect, "http://"+address, nil)
	if err != nil {
		return err
	}
	request.Host = address
	setProxyAuthorization(request.Header, proxyURL)
	if err := request.Write(connection); err != nil {
		return err
	}
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return fmt.Errorf("proxy CONNECT returned HTTP %d", response.StatusCode)
	}
	return nil
}

func writeOriginRequest(writer io.Writer, request *http.Request) error {
	if _, err := fmt.Fprintf(writer, "%s %s HTTP/1.1\r\n", request.Method, request.URL.RequestURI()); err != nil {
		return err
	}
	return writeRequestHeaders(writer, request, nil)
}

func writeRequestHeaders(writer io.Writer, request *http.Request, proxyURL *url.URL) error {
	if _, err := fmt.Fprintf(writer, "Host: %s\r\n", request.URL.Host); err != nil {
		return err
	}
	headers := request.Header.Clone()
	headers.Set("Connection", "close")
	if proxyURL != nil {
		setProxyAuthorization(headers, proxyURL)
	}
	if err := headers.Write(writer); err != nil {
		return err
	}
	_, err := io.WriteString(writer, "\r\n")
	return err
}

func setProxyAuthorization(headers http.Header, proxyURL *url.URL) {
	if proxyURL == nil || proxyURL.User == nil {
		return
	}
	password, _ := proxyURL.User.Password()
	credentials := proxyURL.User.Username() + ":" + password
	headers.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(credentials)))
}

type connectionBody struct {
	io.ReadCloser
	connection net.Conn
	stopCancel func() bool
}

func (body *connectionBody) Close() error {
	if body.stopCancel != nil {
		body.stopCancel()
	}
	readErr := body.ReadCloser.Close()
	connectionErr := body.connection.Close()
	if readErr != nil {
		return readErr
	}
	return connectionErr
}
