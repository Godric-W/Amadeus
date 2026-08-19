package config

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestRedactMasksEveryConfiguredAPIKey(t *testing.T) {
	configured := Default()
	openAI := configured.Providers[DefaultProviderName]
	openAI.APIKey = "openai-secret"
	configured.Providers[DefaultProviderName] = openAI
	configured.Providers["compatible"] = ProviderConfig{
		API:               APIChatCompletions,
		APIKey:            "compatible-secret",
		BaseURL:           "https://compatible.example.invalid/v1",
		Timeout:           openAI.Timeout,
		RequestMaxRetries: openAI.RequestMaxRetries,
		StreamMaxRetries:  openAI.StreamMaxRetries,
		StreamIdleTimeout: openAI.StreamIdleTimeout,
		Temperature:       openAI.Temperature,
		MaxOutputTokens:   openAI.MaxOutputTokens,
	}
	configured.Web.Search.APIKey = "web-search-secret"

	redacted := Redact(configured)
	for name, provider := range redacted.Providers {
		if provider.APIKey != RedactedSecret {
			t.Fatalf("provider %q API key was not redacted: got %q", name, provider.APIKey)
		}
	}
	if redacted.Web.Search.APIKey != RedactedSecret {
		t.Fatalf("Web search API key was not redacted: got %q", redacted.Web.Search.APIKey)
	}
}

func TestRedactDoesNotMutateOriginalConfig(t *testing.T) {
	configured := Default()
	provider := configured.Providers[DefaultProviderName]
	provider.APIKey = "original-secret"
	configured.Providers[DefaultProviderName] = provider

	redacted := Redact(configured)
	if configured.Providers[DefaultProviderName].APIKey != "original-secret" {
		t.Fatal("redaction mutated the original config")
	}
	if redacted.Providers[DefaultProviderName].APIKey != RedactedSecret {
		t.Fatalf("unexpected redacted API key: got %q", redacted.Providers[DefaultProviderName].APIKey)
	}
}

func TestRedactPreservesUnsetAPIKeys(t *testing.T) {
	configured := Default()
	redacted := Redact(configured)

	if redacted.Providers[DefaultProviderName].APIKey != "" {
		t.Fatalf("unset API key should remain empty: got %q", redacted.Providers[DefaultProviderName].APIKey)
	}
}

func TestRedactedYAMLDoesNotContainSecrets(t *testing.T) {
	configured := Default()
	provider := configured.Providers[DefaultProviderName]
	provider.APIKey = "highly-sensitive-secret"
	configured.Providers[DefaultProviderName] = provider

	encoded, err := yaml.Marshal(Redact(configured))
	if err != nil {
		t.Fatalf("marshal redacted config: %v", err)
	}

	output := string(encoded)
	if strings.Contains(output, "highly-sensitive-secret") {
		t.Fatalf("redacted YAML contains the original secret: %s", output)
	}
	if !strings.Contains(output, RedactedSecret) {
		t.Fatalf("redacted YAML does not contain the redaction marker: %s", output)
	}
}
