package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadReturnsFieldDefaultsWhenFileDoesNotExist(t *testing.T) {
	configured, err := newTestLoader(t.TempDir()).Load()
	if err != nil {
		t.Fatalf("load missing config: %v", err)
	}
	if !reflect.DeepEqual(configured, Default()) {
		t.Fatalf("missing config did not return field defaults: %#v", configured)
	}
}

func TestLoadPreservesExplicitZeroWebRedirectLimit(t *testing.T) {
	loader := newTestLoader(t.TempDir())
	writeConfig(t, loader, `
web:
  fetch:
    enabled: true
    max_redirects: 0
`)
	configured, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !configured.Web.Fetch.Enabled || configured.Web.Fetch.MaxRedirects != 0 {
		t.Fatalf("explicit redirect disable was not preserved: %#v", configured.Web.Fetch)
	}
}

func TestLoadReadsConfigV2AndAppliesProviderDefaults(t *testing.T) {
	loader := newTestLoader(t.TempDir())
	writeConfig(t, loader, `
version: 2
model: compatible-model
model_provider: compatible
model_context_window: 128000
model_providers:
  compatible:
    wire_api: chat_completions
    api_key: test-key
    base_url: https://example.invalid/v1
    timeout: 45s
    stream_max_retries: 2
agent:
  max_parallel_tools: 2
`)

	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load config v2: %v", err)
	}
	provider := configured.ModelProviders["compatible"]
	if configured.Model != "compatible-model" || configured.ModelProvider != "compatible" || configured.ModelContextWindow != 128_000 {
		t.Fatalf("unexpected model config: %#v", configured)
	}
	if provider.WireAPI != WireAPIChatCompletions || provider.Dialect != DialectStandard || provider.Timeout != 45*time.Second {
		t.Fatalf("unexpected provider config: %#v", provider)
	}
	if provider.RequestMaxRetries != 4 || provider.StreamMaxRetries != 2 || provider.StreamIdleTimeout != 5*time.Minute {
		t.Fatalf("provider defaults were not normalized: %#v", provider)
	}
	if configured.ToolOutputTokenLimit != 10_000 || configured.Agent.MaxParallelTools != 2 {
		t.Fatalf("unexpected field defaults: %#v", configured)
	}
}

func TestLoadRejectsLegacyConfigSchema(t *testing.T) {
	tests := []struct {
		name    string
		content string
		field   string
	}{
		{name: "version one", content: "version: 1\n", field: "version"},
		{name: "default provider", content: "default_provider: compatible\n", field: "default_provider"},
		{name: "providers", content: "providers: {}\n", field: "providers"},
		{name: "provider api", content: "model_providers:\n  compatible:\n    api: responses\n", field: "api"},
		{name: "provider retries", content: "model_providers:\n  compatible:\n    max_retries: 2\n", field: "max_retries"},
		{name: "provider model", content: "model_providers:\n  compatible:\n    model: old-model\n", field: "model"},
		{name: "provider context", content: "model_providers:\n  compatible:\n    context_window: 128000\n", field: "context_window"},
		{name: "provider compact", content: "model_providers:\n  compatible:\n    auto_compact_token_limit: 100000\n", field: "auto_compact_token_limit"},
		{name: "provider tool output", content: "model_providers:\n  compatible:\n    tool_output_max_tokens: 9000\n", field: "tool_output_max_tokens"},
		{name: "provider temperature", content: "model_providers:\n  compatible:\n    temperature: 0.2\n", field: "temperature"},
		{name: "provider max output", content: "model_providers:\n  compatible:\n    max_output_tokens: 8192\n", field: "max_output_tokens"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loader := newTestLoader(t.TempDir())
			writeConfig(t, loader, test.content)
			_, err := loader.Load()
			if err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("legacy field %q was not rejected: %v", test.field, err)
			}
		})
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	loader := newTestLoader(t.TempDir())
	writeConfig(t, loader, "unknown_field: true\n")
	_, err := loader.Load()
	if err == nil || !strings.Contains(err.Error(), "field unknown_field not found") {
		t.Fatalf("expected unknown field error: %v", err)
	}
}

func TestLoadExpandsEnvironmentReferences(t *testing.T) {
	loader := NewLoader(t.TempDir()).WithEnvLookup(mapEnvLookup(map[string]string{
		"MODEL_NAME": "environment-model",
		"API_KEY":    "environment-secret",
	}))
	writeConfig(t, loader, `
model: ${MODEL_NAME}
model_provider: compatible
model_context_window: 64000
model_providers:
  compatible:
    api_key: ${API_KEY}
    base_url: https://example.invalid/v1
`)
	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load environment references: %v", err)
	}
	if configured.Model != "environment-model" || configured.ModelProviders["compatible"].APIKey != "environment-secret" {
		t.Fatalf("environment references were not expanded: %#v", configured)
	}
}

func TestLoadReportsMissingEnvironmentVariableWithFieldPath(t *testing.T) {
	loader := newTestLoader(t.TempDir())
	writeConfig(t, loader, `
model_providers:
  compatible:
    api_key: ${MISSING_KEY}
`)
	_, err := loader.Load()
	if err == nil || !strings.Contains(err.Error(), "model_providers.compatible.api_key") || !strings.Contains(err.Error(), "MISSING_KEY") {
		t.Fatalf("expected field-specific environment error: %v", err)
	}
}

func TestLoadAppliesDirectEnvironmentOverrides(t *testing.T) {
	loader := NewLoader(t.TempDir()).WithEnvLookup(mapEnvLookup(map[string]string{
		EnvModelProvider:        "runtime",
		EnvModel:                "runtime-model",
		EnvModelReasoningEffort: "high",
		EnvWireAPI:              string(WireAPIChatCompletions),
		EnvDialect:              string(DialectDeepSeek),
		EnvAPIKey:               "runtime-secret",
		EnvBaseURL:              "https://runtime.example/v1",
	}))
	writeConfig(t, loader, `
model: file-model
model_provider: file
model_context_window: 128000
model_providers:
  file:
    base_url: https://file.example/v1
`)
	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load environment overrides: %v", err)
	}
	provider := configured.ModelProviders["runtime"]
	if configured.ModelProvider != "runtime" || configured.Model != "runtime-model" {
		t.Fatalf("model overrides did not win: %#v", configured)
	}
	if configured.ModelReasoningEffort == nil || *configured.ModelReasoningEffort != "high" {
		t.Fatalf("reasoning effort override did not win: %#v", configured.ModelReasoningEffort)
	}
	if provider.WireAPI != WireAPIChatCompletions || provider.Dialect != DialectDeepSeek || provider.APIKey != "runtime-secret" || provider.BaseURL != "https://runtime.example/v1" {
		t.Fatalf("provider overrides did not win: %#v", provider)
	}
	if configured.ModelProviders["file"].BaseURL != "https://file.example/v1" {
		t.Fatalf("unselected provider was modified: %#v", configured.ModelProviders["file"])
	}
}

func TestLoadModelReasoningEffortRemainsUnsetWhenOmitted(t *testing.T) {
	loader := newTestLoader(t.TempDir())
	writeConfig(t, loader, `
model: test-model
model_provider: compatible
model_context_window: 128000
model_providers:
  compatible:
    base_url: https://example.invalid/v1
`)
	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if configured.ModelReasoningEffort != nil {
		t.Fatalf("omitted reasoning effort was synthesized: %q", *configured.ModelReasoningEffort)
	}
}

func TestLoadModelReasoningEffortFromYAML(t *testing.T) {
	loader := newTestLoader(t.TempDir())
	writeConfig(t, loader, `
model: test-model
model_provider: compatible
model_context_window: 128000
model_reasoning_effort: xhigh
model_providers:
  compatible:
    base_url: https://example.invalid/v1
`)
	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if configured.ModelReasoningEffort == nil || *configured.ModelReasoningEffort != "xhigh" {
		t.Fatalf("unexpected reasoning effort: %#v", configured.ModelReasoningEffort)
	}
}

func TestExplicitConfigPathMustExist(t *testing.T) {
	_, err := newTestFileLoader(filepath.Join(t.TempDir(), "missing.yaml")).Load()
	if err == nil || !strings.Contains(err.Error(), "open config") {
		t.Fatalf("expected missing explicit config error: %v", err)
	}
}

func mapEnvLookup(values map[string]string) EnvLookup {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func newTestLoader(amadeusRoot string) Loader {
	return NewLoader(amadeusRoot).WithEnvLookup(mapEnvLookup(nil))
}

func newTestFileLoader(path string) Loader {
	return NewFileLoader(path).WithEnvLookup(mapEnvLookup(nil))
}

func writeConfig(t *testing.T, loader Loader, content string) {
	t.Helper()
	path := loader.ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
