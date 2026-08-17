package config

import (
	"testing"
	"time"
)

func TestConfigCanBeConstructed(t *testing.T) {
	configured := Config{
		Version:         1,
		DefaultProvider: "compatible",
		Providers: map[string]ProviderConfig{
			"compatible": {
				API:             APIChatCompletions,
				Dialect:         DialectDeepSeek,
				APIKey:          "test-key",
				BaseURL:         "https://example.invalid/v1",
				Model:           "test-model",
				Timeout:         30 * time.Second,
				MaxRetries:      1,
				Temperature:     0.5,
				MaxOutputTokens: 4096,
			},
		},
		Agent: AgentConfig{
			MaxParallelTools: 2,
		},
		Logging: LoggingConfig{
			Level:    LogLevelDebug,
			TraceLLM: true,
		},
	}

	provider := configured.Providers[configured.DefaultProvider]
	if provider.API != APIChatCompletions {
		t.Fatalf("unexpected API mode: got %q", provider.API)
	}
	if provider.Dialect != DialectDeepSeek {
		t.Fatalf("unexpected provider dialect: got %q", provider.Dialect)
	}
	if configured.Agent.MaxParallelTools != 2 {
		t.Fatalf("unexpected agent config: %#v", configured.Agent)
	}
}

func TestDefault(t *testing.T) {
	configured := Default()
	provider, ok := configured.Providers[DefaultProviderName]
	if !ok {
		t.Fatalf("default provider %q is missing", DefaultProviderName)
	}

	if configured.Version != CurrentVersion {
		t.Fatalf("unexpected config version: got %d, want %d", configured.Version, CurrentVersion)
	}
	if configured.DefaultProvider != DefaultProviderName {
		t.Fatalf("unexpected default provider: got %q, want %q", configured.DefaultProvider, DefaultProviderName)
	}
	if provider.API != APIResponses {
		t.Fatalf("unexpected default API mode: got %q, want %q", provider.API, APIResponses)
	}
	if provider.Dialect != DialectOpenAI {
		t.Fatalf("unexpected default dialect: got %q, want %q", provider.Dialect, DialectOpenAI)
	}
	if provider.Timeout != 2*time.Minute {
		t.Fatalf("unexpected default timeout: got %s", provider.Timeout)
	}
	if configured.Agent.MaxParallelTools != 4 {
		t.Fatalf("unexpected default agent config: %#v", configured.Agent)
	}
	if configured.Logging.Level != LogLevelInfo {
		t.Fatalf("unexpected log level: got %q, want %q", configured.Logging.Level, LogLevelInfo)
	}
}

func TestDefaultReturnsIndependentProviderMaps(t *testing.T) {
	first := Default()
	second := Default()

	delete(first.Providers, DefaultProviderName)

	if _, ok := second.Providers[DefaultProviderName]; !ok {
		t.Fatal("mutating one default config changed another default config")
	}
}
