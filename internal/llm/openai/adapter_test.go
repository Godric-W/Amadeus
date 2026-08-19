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
	provider.WireAPI = config.WireAPIResponses
	provider.Dialect = config.DialectOpenAI
	adapter, err := NewAdapter("openai", "test-model", provider)
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

	provider.WireAPI = config.WireAPIChatCompletions
	provider.Dialect = config.DialectStandard
	adapter, err = NewAdapter("compatible", "test-model", provider)
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
		api       config.WireAPI
		reasoning bool
	}{
		{name: "standard responses", dialect: config.DialectStandard, api: config.WireAPIResponses},
		{name: "OpenAI responses", dialect: config.DialectOpenAI, api: config.WireAPIResponses, reasoning: true},
		{name: "DeepSeek chat", dialect: config.DialectDeepSeek, api: config.WireAPIChatCompletions, reasoning: true},
		{name: "Qwen chat", dialect: config.DialectQwen, api: config.WireAPIChatCompletions, reasoning: true},
		{name: "GLM chat", dialect: config.DialectGLM, api: config.WireAPIChatCompletions, reasoning: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := validAdapterProvider()
			provider.WireAPI = test.api
			provider.Dialect = test.dialect

			adapter, err := NewAdapter("provider-name-does-not-select-dialect", "test-model", provider)
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
		api     config.WireAPI
	}{
		{name: "unknown dialect", dialect: "vendor-specific", api: config.WireAPIChatCompletions},
		{name: "DeepSeek Responses", dialect: config.DialectDeepSeek, api: config.WireAPIResponses},
		{name: "Qwen Responses", dialect: config.DialectQwen, api: config.WireAPIResponses},
		{name: "GLM Responses", dialect: config.DialectGLM, api: config.WireAPIResponses},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := validAdapterProvider()
			provider.WireAPI = test.api
			provider.Dialect = test.dialect

			_, err := NewAdapter("provider", "test-model", provider)
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
		provider   config.ModelProviderInfo
		providerID string
		model      string
		errorMatch string
	}{
		{name: "empty provider name", model: "test-model", provider: validAdapterProvider(), errorMatch: "provider name"},
		{name: "empty model", providerID: "openai", provider: validAdapterProvider(), errorMatch: "model"},
		{name: "invalid API", providerID: "openai", provider: func() config.ModelProviderInfo {
			provider := validAdapterProvider()
			provider.WireAPI = "legacy"
			return provider
		}(), model: "test-model", errorMatch: "wire API"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewAdapter(test.providerID, test.model, test.provider)
			if err == nil || !strings.Contains(err.Error(), test.errorMatch) {
				t.Fatalf("unexpected adapter error: %v", err)
			}
		})
	}
}

func TestAdapterRejectsImagesBeforeCallingUnsupportedProvider(t *testing.T) {
	provider := validAdapterProvider()
	provider.WireAPI = config.WireAPIChatCompletions
	provider.Dialect = config.DialectDeepSeek
	adapter, err := NewAdapter("deepseek", "test-model", provider)
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Stream(context.Background(), llm.Request{
		Model: "test-model", Prompt: llm.Prompt{Input: []llm.ResponseItem{{Role: llm.RoleUser, Parts: []llm.ContentPart{llm.ImagePart("image/png", "YQ==")}}}},
	})
	var providerError *llm.ProviderError
	if !errors.As(err, &providerError) || providerError.Kind != llm.ProviderErrorInvalidRequest || !strings.Contains(providerError.Message, "does not support image") {
		t.Fatalf("unexpected image capability error: %v", err)
	}
}

func validAdapterProvider() config.ModelProviderInfo {
	provider := configuredProvider()
	provider.WireAPI = config.WireAPIResponses
	provider.Dialect = config.DialectOpenAI
	return provider
}
