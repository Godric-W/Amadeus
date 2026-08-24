package config

import (
	"reflect"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestConfigCanBeConstructed(t *testing.T) {
	configured := Config{
		Model:                            "test-model",
		ModelProvider:                    "compatible",
		ModelContextWindow:               128_000,
		ModelInputModalities:             []llm.InputModality{llm.InputModalityText, llm.InputModalityImage},
		ModelSupportsOriginalImageDetail: true,
		ToolOutputTokenLimit:             DefaultToolOutputTokenLimit,
		ModelProviders: map[string]ModelProviderInfo{
			"compatible": {
				WireAPI:           WireAPIChatCompletions,
				Dialect:           DialectDeepSeek,
				APIKey:            "test-key",
				BaseURL:           "https://example.invalid/v1",
				Timeout:           30 * time.Second,
				RequestMaxRetries: 1,
				StreamMaxRetries:  2,
				StreamIdleTimeout: time.Minute,
			},
		},
		Agent: AgentConfig{MaxParallelTools: 2, MultiAgent: MultiAgentConfig{
			Enabled: true, MaxAgents: 3, MaxDepth: 1, ChildMaxSamples: 10, ChildMaxToolCalls: 50, ChildMaxDuration: time.Minute,
		}},
		Logging: LoggingConfig{Level: LogLevelDebug, TraceLLM: true},
	}

	provider := configured.ModelProviders[configured.ModelProvider]
	if provider.WireAPI != WireAPIChatCompletions || provider.Dialect != DialectDeepSeek {
		t.Fatalf("unexpected provider: %#v", provider)
	}
	if configured.Model != "test-model" || configured.Agent.MaxParallelTools != 2 {
		t.Fatalf("unexpected model config: %#v", configured)
	}
}

func TestDefaultContainsFieldDefaultsWithoutBuiltInProvider(t *testing.T) {
	configured := Default()
	if configured.Model != "" || configured.ModelProvider != "" || len(configured.ModelProviders) != 0 {
		t.Fatalf("default config must not synthesize a model provider: %#v", configured)
	}
	if configured.ToolOutputTokenLimit != 10_000 {
		t.Fatalf("unexpected tool output token limit: %d", configured.ToolOutputTokenLimit)
	}
	if !reflect.DeepEqual(configured.ModelInputModalities, []llm.InputModality{llm.InputModalityText}) || configured.ModelSupportsOriginalImageDetail {
		t.Fatalf("unexpected default model capabilities: %#v", configured)
	}
	if configured.ModelReasoningEffort != nil {
		t.Fatalf("default config must not synthesize reasoning effort: %q", *configured.ModelReasoningEffort)
	}
	if configured.Agent.MaxParallelTools != 4 || configured.Agent.MultiAgent.MaxAgents != 4 || configured.Agent.MultiAgent.MaxDepth != 1 || !configured.Agent.MultiAgent.Enabled || configured.Logging.Level != LogLevelInfo {
		t.Fatalf("unexpected field defaults: %#v", configured)
	}
}

func TestConfigSourcesDoNotExposeSchemaVersion(t *testing.T) {
	if _, exists := SourcesFor(Default())["version"]; exists {
		t.Fatal("configuration provenance contains removed schema version")
	}
}

func TestConfigCloneCopiesReasoningEffort(t *testing.T) {
	effort := llm.ReasoningEffortHigh
	configured := Default()
	configured.ModelReasoningEffort = &effort
	cloned := clone(configured)
	*cloned.ModelReasoningEffort = llm.ReasoningEffortLow
	if *configured.ModelReasoningEffort != llm.ReasoningEffortHigh {
		t.Fatalf("clone changed original effort: %q", *configured.ModelReasoningEffort)
	}
}

func TestConfigCloneCopiesInputModalities(t *testing.T) {
	configured := Default()
	cloned := clone(configured)
	cloned.ModelInputModalities[0] = llm.InputModalityImage
	if configured.ModelInputModalities[0] != llm.InputModalityText {
		t.Fatalf("clone changed original modalities: %#v", configured.ModelInputModalities)
	}
}

func TestDefaultModelProviderInfo(t *testing.T) {
	provider := defaultModelProviderInfo()
	want := ModelProviderInfo{
		WireAPI:           WireAPIResponses,
		Dialect:           DialectStandard,
		Timeout:           2 * time.Minute,
		RequestMaxRetries: 4,
		StreamMaxRetries:  5,
		StreamIdleTimeout: 5 * time.Minute,
	}
	if !reflect.DeepEqual(provider, want) {
		t.Fatalf("unexpected provider defaults: got %#v, want %#v", provider, want)
	}
}

func TestDefaultReturnsIndependentProviderMaps(t *testing.T) {
	first := Default()
	second := Default()
	first.ModelProviders["local"] = defaultModelProviderInfo()
	if len(second.ModelProviders) != 0 {
		t.Fatal("mutating one default config changed another default config")
	}
}
