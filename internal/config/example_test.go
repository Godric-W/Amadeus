package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestExampleConfigLoadsAndValidatesWithoutEnvironment(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate example config test file")
	}

	path := filepath.Join(filepath.Dir(testFile), "..", "..", "configs", "config.yaml.example")
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

	if configured.ModelProvider != "openai" {
		t.Fatalf("unexpected model provider: got %q", configured.ModelProvider)
	}
	if configured.ModelProviders["openai"].WireAPI != WireAPIResponses {
		t.Fatalf("unexpected OpenAI wire API: got %q", configured.ModelProviders["openai"].WireAPI)
	}
	if configured.ModelProviders["compatible"].WireAPI != WireAPIChatCompletions {
		t.Fatalf("unexpected compatible API mode: got %q", configured.ModelProviders["compatible"].WireAPI)
	}
	if configured.Agent.MaxParallelTools != 4 {
		t.Fatalf("unexpected example Agent config: %#v", configured.Agent)
	}
	if len(configured.ModelInputModalities) != 1 || configured.ModelInputModalities[0] != "text" || configured.ModelSupportsOriginalImageDetail {
		t.Fatalf("unexpected example model capabilities: %#v", configured)
	}
	for name, provider := range configured.ModelProviders {
		if provider.APIKey != "" {
			t.Fatalf("example provider %q contains an API key", name)
		}
	}
}
