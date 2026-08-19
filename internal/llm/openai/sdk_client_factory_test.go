package openai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestNewSDKClientAppliesProviderConfiguration(t *testing.T) {
	provider := configuredProvider()
	var captured *http.Request
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request.Clone(request.Context())
		return jsonResponse(http.StatusOK, `{"object":"list","data":[]}`), nil
	})}

	client, err := newSDKClient(provider, httpClient)
	if err != nil {
		t.Fatalf("create SDK client: %v", err)
	}
	if _, err := client.Models.List(context.Background()); err != nil {
		t.Fatalf("list models through configured client: %v", err)
	}

	if captured == nil {
		t.Fatal("configured client did not issue a request")
	}
	if captured.URL.String() != "https://gateway.example.invalid/v1/models" {
		t.Fatalf("unexpected request URL: %s", captured.URL)
	}
	if captured.Header.Get("Authorization") != "Bearer test-secret" {
		t.Fatalf("unexpected authorization header: %q", captured.Header.Get("Authorization"))
	}
	if _, ok := captured.Context().Deadline(); ok {
		t.Fatal("provider timeout must not impose a wall-clock deadline on streaming requests")
	}
}

func TestNewSDKClientAppliesRequestMaxRetries(t *testing.T) {
	provider := configuredProvider()
	provider.RequestMaxRetries = 2
	attempts := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		attempts++
		if attempts <= provider.RequestMaxRetries {
			response := jsonResponse(http.StatusInternalServerError, `{"error":{"message":"retry"}}`)
			response.Header.Set("Retry-After-Ms", "0")
			return response, nil
		}
		return jsonResponse(http.StatusOK, `{"object":"list","data":[]}`), nil
	})}

	client, err := newSDKClient(provider, httpClient)
	if err != nil {
		t.Fatalf("create SDK client: %v", err)
	}
	if _, err := client.Models.List(context.Background()); err != nil {
		t.Fatalf("list models after retries: %v", err)
	}
	if attempts != provider.RequestMaxRetries+1 {
		t.Fatalf("unexpected request attempts: got %d, want %d", attempts, provider.RequestMaxRetries+1)
	}
}

func TestProviderHTTPClientUsesResponseHeaderTimeout(t *testing.T) {
	client := providerHTTPClient(75 * time.Millisecond)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected provider transport type: %T", client.Transport)
	}
	if transport.ResponseHeaderTimeout != 75*time.Millisecond {
		t.Fatalf("unexpected response header timeout: %s", transport.ResponseHeaderTimeout)
	}
	if client.Timeout != 0 {
		t.Fatalf("provider HTTP client must not impose a whole-stream timeout: %s", client.Timeout)
	}
}

func TestNewSDKClientRejectsInvalidRuntimeConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*config.ModelProviderInfo)
		errorMatch string
	}{
		{name: "missing API key", configure: func(provider *config.ModelProviderInfo) { provider.APIKey = "" }, errorMatch: "API key"},
		{name: "missing base URL", configure: func(provider *config.ModelProviderInfo) { provider.BaseURL = "" }, errorMatch: "base URL"},
		{name: "invalid base URL", configure: func(provider *config.ModelProviderInfo) { provider.BaseURL = "relative" }, errorMatch: "absolute URL"},
		{name: "invalid scheme", configure: func(provider *config.ModelProviderInfo) { provider.BaseURL = "ftp://example.invalid" }, errorMatch: "scheme"},
		{name: "invalid timeout", configure: func(provider *config.ModelProviderInfo) { provider.Timeout = 0 }, errorMatch: "timeout"},
		{name: "invalid retries", configure: func(provider *config.ModelProviderInfo) { provider.RequestMaxRetries = -1 }, errorMatch: "retries"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := configuredProvider()
			test.configure(&provider)

			_, err := newSDKClient(provider, nil)
			if err == nil {
				t.Fatal("expected client factory error")
			}
			if !strings.Contains(err.Error(), test.errorMatch) {
				t.Fatalf("unexpected client factory error: %v", err)
			}
		})
	}
}

func configuredProvider() config.ModelProviderInfo {
	return config.ModelProviderInfo{
		WireAPI:           config.WireAPIResponses,
		Dialect:           config.DialectOpenAI,
		APIKey:            "test-secret",
		BaseURL:           "https://gateway.example.invalid/v1",
		Timeout:           75 * time.Millisecond,
		RequestMaxRetries: 0,
		StreamMaxRetries:  5,
		StreamIdleTimeout: 5 * time.Minute,
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
