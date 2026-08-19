package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadReturnsDefaultWhenFileDoesNotExist(t *testing.T) {
	configured, err := newTestLoader(t.TempDir()).Load()
	if err != nil {
		t.Fatalf("load missing project config: %v", err)
	}

	if !reflect.DeepEqual(configured, Default()) {
		t.Fatalf("missing project config did not return defaults: got %#v", configured)
	}
}

func TestLoadReadsConfigFromAmadeusRoot(t *testing.T) {
	amadeusRoot := t.TempDir()
	loader := newTestLoader(amadeusRoot)
	writeConfig(t, loader, `
version: 1
default_provider: compatible
providers:
  openai:
    model: configured-openai-model
  compatible:
    api: chat_completions
    api_key: test-key
    base_url: https://example.invalid/v1
    model: compatible-model
    timeout: 45s
    request_max_retries: 1
    stream_max_retries: 2
    stream_idle_timeout: 90s
    temperature: 0.5
    max_output_tokens: 4096
agent:
  max_parallel_tools: 2
web:
  fetch:
    enabled: true
    timeout: 20s
    max_bytes: 2097152
    max_redirects: 2
  search:
    enabled: true
    provider: brave
    api_key: brave-key
    timeout: 12s
    max_results: 7
logging:
  level: debug
  trace_llm: true
`)

	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load project config: %v", err)
	}

	compatible := configured.Providers["compatible"]
	if configured.DefaultProvider != "compatible" {
		t.Fatalf("unexpected default provider: got %q", configured.DefaultProvider)
	}
	if compatible.API != APIChatCompletions {
		t.Fatalf("unexpected compatible API mode: got %q", compatible.API)
	}
	if compatible.Timeout != 45*time.Second {
		t.Fatalf("unexpected compatible timeout: got %s", compatible.Timeout)
	}
	if compatible.RequestMaxRetries != 1 || compatible.StreamMaxRetries != 2 || compatible.StreamIdleTimeout != 90*time.Second {
		t.Fatalf("unexpected compatible retry config: %#v", compatible)
	}
	if configured.Agent.MaxParallelTools != 2 {
		t.Fatalf("unexpected agent config: %#v", configured.Agent)
	}
	if !configured.Web.Fetch.Enabled || configured.Web.Fetch.Timeout != 20*time.Second || configured.Web.Fetch.MaxBytes != 2<<20 || configured.Web.Fetch.MaxRedirects != 2 {
		t.Fatalf("unexpected Web fetch config: %#v", configured.Web.Fetch)
	}
	if !configured.Web.Search.Enabled || configured.Web.Search.Provider != WebSearchBrave || configured.Web.Search.APIKey != "brave-key" || configured.Web.Search.Timeout != 12*time.Second || configured.Web.Search.MaxResults != 7 {
		t.Fatalf("unexpected Web search config: %#v", configured.Web.Search)
	}
	if configured.Logging.Level != LogLevelDebug || !configured.Logging.TraceLLM {
		t.Fatalf("unexpected logging config: %#v", configured.Logging)
	}
}

func TestLoadMigratesLegacyMaxRetriesToRequestRetriesOnly(t *testing.T) {
	loader := newTestLoader(t.TempDir())
	writeConfig(t, loader, `
providers:
  openai:
    model: configured-model
    max_retries: 2
`)

	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load legacy retry config: %v", err)
	}
	provider := configured.Providers[DefaultProviderName]
	if provider.RequestMaxRetries != 2 {
		t.Fatalf("legacy max_retries did not migrate to request retries: %#v", provider)
	}
	if provider.StreamMaxRetries != 5 {
		t.Fatalf("legacy max_retries changed stream retries: %#v", provider)
	}
}

func TestLoadRejectsLegacyAndCurrentRequestRetriesTogether(t *testing.T) {
	loader := newTestLoader(t.TempDir())
	writeConfig(t, loader, `
providers:
  openai:
    max_retries: 2
    request_max_retries: 3
`)

	_, err := loader.Load()
	if err == nil || !strings.Contains(err.Error(), "cannot both be set") {
		t.Fatalf("expected conflicting retry fields to fail clearly: %v", err)
	}
}

func TestLoadPreservesUnspecifiedProviderDefaults(t *testing.T) {
	amadeusRoot := t.TempDir()
	loader := newTestLoader(amadeusRoot)
	writeConfig(t, loader, `
providers:
  openai:
    model: configured-model
`)

	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load project config: %v", err)
	}

	provider := configured.Providers[DefaultProviderName]
	if provider.Model != "configured-model" {
		t.Fatalf("unexpected configured model: got %q", provider.Model)
	}
	if provider.API != APIResponses {
		t.Fatalf("default API mode was not preserved: got %q", provider.API)
	}
	if provider.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("default base URL was not preserved: got %q", provider.BaseURL)
	}
	if provider.MaxOutputTokens != 8192 {
		t.Fatalf("default max output tokens were not preserved: got %d", provider.MaxOutputTokens)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	amadeusRoot := t.TempDir()
	loader := newTestLoader(amadeusRoot)
	writeConfig(t, loader, "unknown_field: true\n")

	_, err := loader.Load()
	if err == nil {
		t.Fatal("expected unknown field error")
	}
	if !strings.Contains(err.Error(), loader.ConfigPath()) {
		t.Fatalf("error does not contain config path: %v", err)
	}
}

func TestLoadRejectsRemovedAgentTokenBudgets(t *testing.T) {
	amadeusRoot := t.TempDir()
	loader := newTestLoader(amadeusRoot)
	writeConfig(t, loader, `
agent:
  max_input_tokens: 1000000
  max_output_tokens: 245760
`)

	_, err := loader.Load()
	if err == nil || !strings.Contains(err.Error(), "max_input_tokens") || !strings.Contains(err.Error(), "max_output_tokens") {
		t.Fatalf("removed Agent token budgets were not rejected clearly: %v", err)
	}
}

func TestLoadRejectsRemovedAgentMode(t *testing.T) {
	loader := newTestLoader(t.TempDir())
	writeConfig(t, loader, `
agent:
  mode: react
`)

	_, err := loader.Load()
	if err == nil {
		t.Fatal("expected removed agent mode field to be rejected")
	}
	if !strings.Contains(err.Error(), "field mode not found") {
		t.Fatalf("unexpected removed agent mode error: %v", err)
	}
}

func TestConfigPathIsInAmadeusRoot(t *testing.T) {
	amadeusRoot := t.TempDir()
	loader := newTestLoader(amadeusRoot)
	expected := filepath.Join(amadeusRoot, configFileName)

	if loader.ConfigPath() != expected {
		t.Fatalf("unexpected config path: got %q, want %q", loader.ConfigPath(), expected)
	}
}

func TestLoadReadsExplicitConfigPath(t *testing.T) {
	amadeusRoot := t.TempDir()
	path := filepath.Join(amadeusRoot, "configs", "development.yaml")
	loader := newTestFileLoader(path)
	writeConfig(t, loader, `
default_provider: local
providers:
  local:
    api: chat_completions
    base_url: http://localhost:11434/v1
    model: local-model
`)

	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load explicit config: %v", err)
	}

	if loader.ConfigPath() != path {
		t.Fatalf("unexpected explicit config path: got %q, want %q", loader.ConfigPath(), path)
	}
	if configured.DefaultProvider != "local" {
		t.Fatalf("unexpected default provider: got %q", configured.DefaultProvider)
	}
	if configured.Providers["local"].Model != "local-model" {
		t.Fatalf("unexpected local provider: %#v", configured.Providers["local"])
	}
}

func TestExplicitConfigPathMustExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")

	_, err := newTestFileLoader(path).Load()
	if err == nil {
		t.Fatal("expected missing explicit config error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error does not contain explicit config path: %v", err)
	}
}

func TestLoadExpandsEnvironmentReferences(t *testing.T) {
	amadeusRoot := t.TempDir()
	loader := NewLoader(amadeusRoot).WithEnvLookup(mapEnvLookup(map[string]string{
		"AMADEUS_API_KEY": "test-secret",
		"AMADEUS_HOST":    "gateway.example.invalid",
		"AMADEUS_MODEL":   "test-model",
		"AMADEUS_TIMEOUT": "75s",
	}))
	writeConfig(t, loader, `
providers:
  openai:
    api_key: ${AMADEUS_API_KEY}
    base_url: https://${AMADEUS_HOST}/v1
    model: ${AMADEUS_MODEL}
    timeout: ${AMADEUS_TIMEOUT}
`)

	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load config with environment references: %v", err)
	}

	provider := configured.Providers[DefaultProviderName]
	if provider.APIKey != "test-secret" {
		t.Fatalf("unexpected expanded API key: got %q", provider.APIKey)
	}
	if provider.BaseURL != "https://gateway.example.invalid/v1" {
		t.Fatalf("unexpected expanded base URL: got %q", provider.BaseURL)
	}
	if provider.Model != "test-model" {
		t.Fatalf("unexpected expanded model: got %q", provider.Model)
	}
	if provider.Timeout != 75*time.Second {
		t.Fatalf("unexpected expanded timeout: got %s", provider.Timeout)
	}
}

func TestLoadAllowsEmptyEnvironmentValues(t *testing.T) {
	amadeusRoot := t.TempDir()
	loader := NewLoader(amadeusRoot).WithEnvLookup(mapEnvLookup(map[string]string{
		"OPTIONAL_MODEL": "",
	}))
	writeConfig(t, loader, `
providers:
  openai:
    model: ${OPTIONAL_MODEL}
`)

	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load config with empty environment value: %v", err)
	}
	if configured.Providers[DefaultProviderName].Model != "" {
		t.Fatalf("expected empty model, got %q", configured.Providers[DefaultProviderName].Model)
	}
}

func TestLoadReportsMissingEnvironmentVariableWithFieldPath(t *testing.T) {
	amadeusRoot := t.TempDir()
	loader := NewLoader(amadeusRoot).WithEnvLookup(mapEnvLookup(nil))
	writeConfig(t, loader, `
providers:
  compatible:
    api_key: ${MISSING_API_KEY}
`)

	_, err := loader.Load()
	if err == nil {
		t.Fatal("expected missing environment variable error")
	}
	if !strings.Contains(err.Error(), "providers.compatible.api_key") {
		t.Fatalf("error does not contain field path: %v", err)
	}
	if !strings.Contains(err.Error(), "MISSING_API_KEY") {
		t.Fatalf("error does not contain variable name: %v", err)
	}
}

func TestLoadAppliesDirectEnvironmentOverridesToSelectedProvider(t *testing.T) {
	amadeusRoot := t.TempDir()
	loader := NewLoader(amadeusRoot).WithEnvLookup(mapEnvLookup(map[string]string{
		EnvProvider: "runtime",
		EnvAPI:      string(APIChatCompletions),
		EnvDialect:  string(DialectQwen),
		EnvAPIKey:   "runtime-secret",
		EnvBaseURL:  "https://runtime.example.invalid/v1",
		EnvModel:    "runtime-model",
	}))
	writeConfig(t, loader, `
default_provider: file
providers:
  file:
    api: responses
    api_key: file-secret
    base_url: https://file.example.invalid/v1
    model: file-model
`)

	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load config with direct environment overrides: %v", err)
	}

	if configured.DefaultProvider != "runtime" {
		t.Fatalf("unexpected environment provider: got %q", configured.DefaultProvider)
	}
	provider := configured.Providers["runtime"]
	if provider.API != APIChatCompletions {
		t.Fatalf("unexpected environment API mode: got %q", provider.API)
	}
	if provider.Dialect != DialectQwen {
		t.Fatalf("unexpected environment dialect: got %q", provider.Dialect)
	}
	if provider.APIKey != "runtime-secret" {
		t.Fatalf("unexpected environment API key: got %q", provider.APIKey)
	}
	if provider.BaseURL != "https://runtime.example.invalid/v1" {
		t.Fatalf("unexpected environment base URL: got %q", provider.BaseURL)
	}
	if provider.Model != "runtime-model" {
		t.Fatalf("unexpected environment model: got %q", provider.Model)
	}
	if configured.Providers["file"].Model != "file-model" {
		t.Fatalf("file provider was unexpectedly modified: %#v", configured.Providers["file"])
	}
}

func TestLoadAppliesEnvironmentOverridesWithoutConfigFile(t *testing.T) {
	loader := NewLoader(t.TempDir()).WithEnvLookup(mapEnvLookup(map[string]string{
		EnvModel: "environment-model",
	}))

	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load defaults with environment overrides: %v", err)
	}
	if configured.Providers[DefaultProviderName].Model != "environment-model" {
		t.Fatalf("environment override was not applied to defaults: %#v", configured.Providers[DefaultProviderName])
	}
}

func TestDirectEnvironmentOverrideWinsOverYAMLReference(t *testing.T) {
	amadeusRoot := t.TempDir()
	loader := NewLoader(amadeusRoot).WithEnvLookup(mapEnvLookup(map[string]string{
		"FILE_MODEL": "file-expanded-model",
		EnvModel:     "direct-environment-model",
	}))
	writeConfig(t, loader, `
providers:
  openai:
    model: ${FILE_MODEL}
`)

	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load config with layered environment values: %v", err)
	}
	if configured.Providers[DefaultProviderName].Model != "direct-environment-model" {
		t.Fatalf("direct environment override did not win: %#v", configured.Providers[DefaultProviderName])
	}
}

func TestDirectEnvironmentOverrideAllowsEmptyValue(t *testing.T) {
	amadeusRoot := t.TempDir()
	loader := NewLoader(amadeusRoot).WithEnvLookup(mapEnvLookup(map[string]string{
		EnvAPIKey: "",
	}))
	writeConfig(t, loader, `
providers:
  openai:
    api_key: file-secret
`)

	configured, err := loader.Load()
	if err != nil {
		t.Fatalf("load config with empty direct environment override: %v", err)
	}
	if configured.Providers[DefaultProviderName].APIKey != "" {
		t.Fatalf("expected empty API key override, got %q", configured.Providers[DefaultProviderName].APIKey)
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
		t.Fatalf("create project config directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write project config: %v", err)
	}
}
