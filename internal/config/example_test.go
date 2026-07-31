package config

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestExampleConfigLoadsAndValidatesWithoutEnvironment(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate example config test file")
	}

	path := filepath.Join(filepath.Dir(testFile), "..", "..", "configs", "amadeus.example.yaml")
	loader := NewFileLoader(path).WithEnvLookup(func(string) (string, bool) {
		return "", false
	})

	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load example config: %v", err)
	}
	if err := Validate(configured); err != nil {
		t.Fatalf("validate example config: %v", err)
	}

	if configured.DefaultProvider != DefaultProviderName {
		t.Fatalf("unexpected default provider: got %q", configured.DefaultProvider)
	}
	if configured.Providers[DefaultProviderName].API != APIResponses {
		t.Fatalf("unexpected OpenAI API mode: got %q", configured.Providers[DefaultProviderName].API)
	}
	if configured.Providers["compatible"].API != APIChatCompletions {
		t.Fatalf("unexpected compatible API mode: got %q", configured.Providers["compatible"].API)
	}
	if configured.Agent.MaxSteps != 30 || configured.Agent.MaxToolCalls != 120 || configured.Agent.MaxInputTokens != 1_000_000 || configured.Agent.MaxOutputTokens != 245_760 || configured.Agent.MaxDuration != 30*time.Minute || configured.Agent.MaxParallelTools != 4 {
		t.Fatalf("unexpected example Agent budget: %#v", configured.Agent)
	}
	for name, provider := range configured.Providers {
		if provider.APIKey != "" {
			t.Fatalf("example provider %q contains an API key", name)
		}
	}
}
