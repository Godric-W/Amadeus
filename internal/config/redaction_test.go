package config

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestRedactMasksEveryConfiguredAPIKey(t *testing.T) {
	configured := Default()
	first := defaultModelProviderInfo()
	first.APIKey = "first-secret"
	second := defaultModelProviderInfo()
	second.APIKey = "second-secret"
	configured.ModelProviders["first"] = first
	configured.ModelProviders["second"] = second
	configured.Web.Search.APIKey = "web-search-secret"

	redacted := Redact(configured)
	for name, provider := range redacted.ModelProviders {
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
	provider := defaultModelProviderInfo()
	provider.APIKey = "original-secret"
	configured.ModelProviders["compatible"] = provider

	redacted := Redact(configured)
	if configured.ModelProviders["compatible"].APIKey != "original-secret" {
		t.Fatal("redaction mutated the original config")
	}
	if redacted.ModelProviders["compatible"].APIKey != RedactedSecret {
		t.Fatalf("unexpected redacted API key: got %q", redacted.ModelProviders["compatible"].APIKey)
	}
}

func TestRedactPreservesUnsetAPIKeys(t *testing.T) {
	configured := Default()
	redacted := Redact(configured)

	if len(redacted.ModelProviders) != 0 {
		t.Fatalf("redaction synthesized providers: %#v", redacted.ModelProviders)
	}
}

func TestRedactedYAMLDoesNotContainSecrets(t *testing.T) {
	configured := Default()
	provider := defaultModelProviderInfo()
	provider.APIKey = "highly-sensitive-secret"
	configured.ModelProviders["compatible"] = provider

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
