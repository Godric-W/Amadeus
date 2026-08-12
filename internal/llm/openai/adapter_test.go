package openai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestNewAdapterExposesStableModelAndCapabilities(t *testing.T) {
	provider := configuredProvider()
	provider.Model = "test-model"
	provider.API = config.APIResponses
	provider.Dialect = config.DialectOpenAI
	adapter, err := NewAdapter("openai", provider)
	if err != nil {
		t.Fatalf("create OpenAI adapter: %v", err)
	}
	if adapter.Model().Provider != "openai" || adapter.Model().Name != "test-model" {
		t.Fatalf("unexpected model info: %#v", adapter.Model())
	}
	capabilities := adapter.Capabilities()
	if !capabilities.SupportsStreaming || !capabilities.SupportsDeveloperRole || !capabilities.SupportsReasoning || !capabilities.SupportsStreamUsage {
		t.Fatalf("unexpected Responses capabilities: %#v", capabilities)
	}

	provider.API = config.APIChatCompletions
	provider.Dialect = config.DialectStandard
	adapter, err = NewAdapter("compatible", provider)
	if err != nil {
		t.Fatalf("create Chat Completions adapter: %v", err)
	}
	if adapter.Capabilities().SupportsReasoning {
		t.Fatalf("standard Chat adapter unexpectedly advertises reasoning: %#v", adapter.Capabilities())
	}
	if adapter.Dialect() != config.DialectStandard {
		t.Fatalf("unexpected selected dialect: got %q", adapter.Dialect())
	}
}

func TestNewAdapterSelectsDialectExplicitly(t *testing.T) {
	tests := []struct {
		name      string
		dialect   config.ProviderDialect
		api       config.APIMode
		reasoning bool
	}{
		{name: "standard responses", dialect: config.DialectStandard, api: config.APIResponses},
		{name: "OpenAI responses", dialect: config.DialectOpenAI, api: config.APIResponses, reasoning: true},
		{name: "DeepSeek chat", dialect: config.DialectDeepSeek, api: config.APIChatCompletions, reasoning: true},
		{name: "Qwen chat", dialect: config.DialectQwen, api: config.APIChatCompletions, reasoning: true},
		{name: "GLM chat", dialect: config.DialectGLM, api: config.APIChatCompletions, reasoning: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := validAdapterProvider()
			provider.API = test.api
			provider.Dialect = test.dialect

			adapter, err := NewAdapter("provider-name-does-not-select-dialect", provider)
			if err != nil {
				t.Fatalf("create adapter: %v", err)
			}
			if adapter.Dialect() != test.dialect {
				t.Fatalf("unexpected dialect: got %q, want %q", adapter.Dialect(), test.dialect)
			}
			if adapter.Capabilities().SupportsReasoning != test.reasoning {
				t.Fatalf("unexpected capabilities for %q: %#v", test.dialect, adapter.Capabilities())
			}
		})
	}
}

func TestNewAdapterRejectsUnsupportedDialectAndAPICombination(t *testing.T) {
	tests := []struct {
		name    string
		dialect config.ProviderDialect
		api     config.APIMode
	}{
		{name: "unknown dialect", dialect: "vendor-specific", api: config.APIChatCompletions},
		{name: "DeepSeek Responses", dialect: config.DialectDeepSeek, api: config.APIResponses},
		{name: "Qwen Responses", dialect: config.DialectQwen, api: config.APIResponses},
		{name: "GLM Responses", dialect: config.DialectGLM, api: config.APIResponses},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := validAdapterProvider()
			provider.API = test.api
			provider.Dialect = test.dialect

			_, err := NewAdapter("provider", provider)
			var dialectError *DialectError
			if !errors.As(err, &dialectError) {
				t.Fatalf("unexpected adapter error: %v", err)
			}
			if dialectError.Dialect != test.dialect {
				t.Fatalf("unexpected dialect error: %#v", dialectError)
			}
		})
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

func TestAdapterRejectsImagesBeforeCallingUnsupportedProvider(t *testing.T) {
	provider := validAdapterProvider()
	provider.API = config.APIChatCompletions
	provider.Dialect = config.DialectDeepSeek
	adapter, err := NewAdapter("deepseek", provider)
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Stream(context.Background(), llm.Request{
		Model: provider.Model, Prompt: llm.Prompt{Input: []llm.Message{{Role: llm.RoleUser, Parts: []llm.ContentPart{llm.ImagePart("image/png", "YQ==")}}}},
		MaxOutputTokens: 10,
	})
	var providerError *llm.ProviderError
	if !errors.As(err, &providerError) || providerError.Kind != llm.ProviderErrorInvalidRequest || !strings.Contains(providerError.Message, "does not support image") {
		t.Fatalf("unexpected image capability error: %v", err)
	}
}

func validAdapterProvider() config.ProviderConfig {
	provider := configuredProvider()
	provider.API = config.APIResponses
	provider.Dialect = config.DialectOpenAI
	provider.Model = "test-model"
	return provider
}
