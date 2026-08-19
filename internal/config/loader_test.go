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

func TestLoadMigratesUnambiguousConfigV1(t *testing.T) {
	loader := newTestLoader(t.TempDir())
	writeConfig(t, loader, `
version: 1
default_provider: compatible
providers:
  compatible:
    api: chat_completions
    base_url: https://example.invalid/v1
    model: compatible-model
    context_window: 128000
    auto_compact_token_limit: 100000
    tool_output_max_tokens: 9000
    max_retries: 2
`)

	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("migrate config v1: %v", err)
	}
	provider := configured.ModelProviders["compatible"]
	if configured.Version != 2 || configured.ModelProvider != "compatible" || configured.Model != "compatible-model" {
		t.Fatalf("top-level migration failed: %#v", configured)
	}
	if configured.ModelContextWindow != 128_000 || configured.ModelAutoCompactTokenLimit != 100_000 || configured.ToolOutputTokenLimit != 9_000 {
		t.Fatalf("model override migration failed: %#v", configured)
	}
	if provider.WireAPI != WireAPIChatCompletions || provider.RequestMaxRetries != 2 || provider.StreamMaxRetries != 5 {
		t.Fatalf("provider migration failed: %#v", provider)
	}
}

func TestLoadRejectsAmbiguousProviderLocalModelMigration(t *testing.T) {
	loader := newTestLoader(t.TempDir())
	writeConfig(t, loader, `
providers:
  first:
    model: model-a
  second:
    model: model-b
`)
	_, err := loader.Load()
	if err == nil || !strings.Contains(err.Error(), "providers define different values") {
		t.Fatalf("expected ambiguous model migration error: %v", err)
	}
}

func TestLoadMigratesEqualProviderLocalModelPolicyValues(t *testing.T) {
	loader := newTestLoader(t.TempDir())
	writeConfig(t, loader, `
default_provider: first
providers:
  first:
    model: shared-model
    context_window: 128000
    auto_compact_token_limit: 100000
    tool_output_max_tokens: 9000
  second:
    model: shared-model
    context_window: 128000
    auto_compact_token_limit: 100000
    tool_output_max_tokens: 9000
`)
	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("migrate equal Provider-local policy: %v", err)
	}
	if configured.Model != "shared-model" || configured.ModelContextWindow != 128_000 || configured.ModelAutoCompactTokenLimit != 100_000 || configured.ToolOutputTokenLimit != 9_000 {
		t.Fatalf("equal Provider-local policy was not promoted: %#v", configured)
	}
}

func TestLoadRejectsTopLevelAndProviderLocalModelPolicyTogether(t *testing.T) {
	tests := []struct {
		current string
		legacy  string
		value   string
	}{
		{current: "model", legacy: "model", value: "shared-model"},
		{current: "model_context_window", legacy: "context_window", value: "128000"},
		{current: "model_auto_compact_token_limit", legacy: "auto_compact_token_limit", value: "100000"},
		{current: "tool_output_token_limit", legacy: "tool_output_max_tokens", value: "9000"},
	}
	for _, test := range tests {
		t.Run(test.current, func(t *testing.T) {
			loader := newTestLoader(t.TempDir())
			writeConfig(t, loader, test.current+": "+test.value+"\nproviders:\n  compatible:\n    "+test.legacy+": "+test.value+"\n")
			_, err := loader.Load()
			if err == nil || !strings.Contains(err.Error(), test.current) || !strings.Contains(err.Error(), test.legacy) || !strings.Contains(err.Error(), "cannot both be set") {
				t.Fatalf("expected %s/%s conflict: %v", test.current, test.legacy, err)
			}
		})
	}
}

func TestLoadRejectsLegacyAndCurrentProviderTransportFieldsTogether(t *testing.T) {
	tests := []struct {
		legacy  string
		current string
		value   string
	}{
		{legacy: "api", current: "wire_api", value: "responses"},
		{legacy: "max_retries", current: "request_max_retries", value: "2"},
	}
	for _, test := range tests {
		t.Run(test.current, func(t *testing.T) {
			loader := newTestLoader(t.TempDir())
			writeConfig(t, loader, "providers:\n  compatible:\n    "+test.legacy+": "+test.value+"\n    "+test.current+": "+test.value+"\n")
			_, err := loader.Load()
			if err == nil || !strings.Contains(err.Error(), "model_providers.compatible") || !strings.Contains(err.Error(), test.current) || !strings.Contains(err.Error(), test.legacy) {
				t.Fatalf("expected Provider transport conflict: %v", err)
			}
		})
	}
}

func TestLoadRejectsRemovedSamplingFields(t *testing.T) {
	for _, field := range []string{"temperature: 0.2", "max_output_tokens: 8192"} {
		t.Run(strings.Split(field, ":")[0], func(t *testing.T) {
			loader := newTestLoader(t.TempDir())
			writeConfig(t, loader, "providers:\n  compatible:\n    "+field+"\n")
			_, err := loader.Load()
			path := "model_providers.compatible." + strings.Split(field, ":")[0]
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "was removed in config version 2") {
				t.Fatalf("expected removed-field error: %v", err)
			}
		})
	}
}

func TestLoadRejectsLegacyAndCurrentFieldsTogether(t *testing.T) {
	loader := newTestLoader(t.TempDir())
	writeConfig(t, loader, `
model_provider: compatible
default_provider: compatible
`)
	_, err := loader.Load()
	if err == nil || !strings.Contains(err.Error(), "cannot both be set") {
		t.Fatalf("expected conflicting field error: %v", err)
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
		EnvModelProvider: "runtime",
		EnvModel:         "runtime-model",
		EnvWireAPI:       string(WireAPIChatCompletions),
		EnvDialect:       string(DialectDeepSeek),
		EnvAPIKey:        "runtime-secret",
		EnvBaseURL:       "https://runtime.example/v1",
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
	if provider.WireAPI != WireAPIChatCompletions || provider.Dialect != DialectDeepSeek || provider.APIKey != "runtime-secret" || provider.BaseURL != "https://runtime.example/v1" {
		t.Fatalf("provider overrides did not win: %#v", provider)
	}
	if configured.ModelProviders["file"].BaseURL != "https://file.example/v1" {
		t.Fatalf("unselected provider was modified: %#v", configured.ModelProviders["file"])
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
