package openai

import (
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
)

func TestNewAdapterExposesStableModelAndCapabilities(t *testing.T) {
	provider := configuredProvider()
	provider.Model = "test-model"
	provider.API = config.APIResponses
	client, err := NewAdapter("openai", provider)
	if err != nil {
		t.Fatalf("create OpenAI adapter: %v", err)
	}
	if client.Model().Provider != "openai" || client.Model().Name != "test-model" {
		t.Fatalf("unexpected model info: %#v", client.Model())
	}
	capabilities := client.Capabilities()
	if !capabilities.SupportsStreaming || !capabilities.SupportsDeveloperRole || !capabilities.SupportsReasoning || !capabilities.SupportsStreamUsage {
		t.Fatalf("unexpected Responses capabilities: %#v", capabilities)
	}

	provider.API = config.APIChatCompletions
	client, err = NewAdapter("compatible", provider)
	if err != nil {
		t.Fatalf("create Chat Completions adapter: %v", err)
	}
	if client.Capabilities().SupportsReasoning {
		t.Fatalf("standard Chat adapter unexpectedly advertises reasoning: %#v", client.Capabilities())
	}
}

func TestNewAdapterRejectsInvalidRuntimeConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		provider   config.ProviderConfig
		providerID string
		errorMatch string
	}{
		{name: "empty provider name", provider: validAdapterProvider(), errorMatch: "provider name"},
		{name: "empty model", providerID: "openai", provider: func() config.ProviderConfig { provider := validAdapterProvider(); provider.Model = ""; return provider }(), errorMatch: "model"},
		{name: "invalid API", providerID: "openai", provider: func() config.ProviderConfig {
			provider := validAdapterProvider()
			provider.API = "legacy"
			return provider
		}(), errorMatch: "API mode"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewAdapter(test.providerID, test.provider)
			if err == nil || !strings.Contains(err.Error(), test.errorMatch) {
				t.Fatalf("unexpected adapter error: %v", err)
			}
		})
	}
}

func validAdapterProvider() config.ProviderConfig {
	provider := configuredProvider()
	provider.API = config.APIResponses
	provider.Model = "test-model"
	return provider
}
