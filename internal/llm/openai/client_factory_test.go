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

func TestNewClientAppliesProviderConfiguration(t *testing.T) {
	provider := configuredProvider()
	var captured *http.Request
	var timeoutRemaining time.Duration
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request.Clone(request.Context())
		if deadline, ok := request.Context().Deadline(); ok {
			timeoutRemaining = time.Until(deadline)
		}
		return jsonResponse(http.StatusOK, `{"object":"list","data":[]}`), nil
	})}

	client, err := newClient(provider, httpClient)
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
	if timeoutRemaining <= 0 || timeoutRemaining > provider.Timeout {
		t.Fatalf("unexpected request timeout: %s", timeoutRemaining)
	}
}

func TestNewClientAppliesMaxRetries(t *testing.T) {
	provider := configuredProvider()
	provider.MaxRetries = 2
	attempts := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		attempts++
		if attempts <= provider.MaxRetries {
			response := jsonResponse(http.StatusInternalServerError, `{"error":{"message":"retry"}}`)
			response.Header.Set("Retry-After-Ms", "0")
			return response, nil
		}
		return jsonResponse(http.StatusOK, `{"object":"list","data":[]}`), nil
	})}

	client, err := newClient(provider, httpClient)
	if err != nil {
		t.Fatalf("create SDK client: %v", err)
	}
	if _, err := client.Models.List(context.Background()); err != nil {
		t.Fatalf("list models after retries: %v", err)
	}
	if attempts != provider.MaxRetries+1 {
		t.Fatalf("unexpected request attempts: got %d, want %d", attempts, provider.MaxRetries+1)
	}
}

func TestNewClientRejectsInvalidRuntimeConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*config.ProviderConfig)
		errorMatch string
	}{
		{name: "missing API key", configure: func(provider *config.ProviderConfig) { provider.APIKey = "" }, errorMatch: "API key"},
		{name: "missing base URL", configure: func(provider *config.ProviderConfig) { provider.BaseURL = "" }, errorMatch: "base URL"},
		{name: "invalid base URL", configure: func(provider *config.ProviderConfig) { provider.BaseURL = "relative" }, errorMatch: "absolute URL"},
		{name: "invalid scheme", configure: func(provider *config.ProviderConfig) { provider.BaseURL = "ftp://example.invalid" }, errorMatch: "scheme"},
		{name: "invalid timeout", configure: func(provider *config.ProviderConfig) { provider.Timeout = 0 }, errorMatch: "timeout"},
		{name: "invalid retries", configure: func(provider *config.ProviderConfig) { provider.MaxRetries = -1 }, errorMatch: "retries"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := configuredProvider()
			test.configure(&provider)

			_, err := NewClient(provider)
			if err == nil {
				t.Fatal("expected client factory error")
			}
			if !strings.Contains(err.Error(), test.errorMatch) {
				t.Fatalf("unexpected client factory error: %v", err)
			}
		})
	}
}

func configuredProvider() config.ProviderConfig {
	provider := config.Default().Providers[config.DefaultProviderName]
	provider.APIKey = "test-secret"
	provider.BaseURL = "https://gateway.example.invalid/v1"
	provider.Timeout = 75 * time.Millisecond
	provider.MaxRetries = 0
	return provider
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
