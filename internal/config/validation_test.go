package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateAcceptsDefaultConfig(t *testing.T) {
	if err := Validate(Default()); err != nil {
		t.Fatalf("validate default config: %v", err)
	}
}

func TestValidateAllowsMissingCredentialsAndModel(t *testing.T) {
	configured := Default()
	provider := configured.Providers[configured.DefaultProvider]
	provider.APIKey = ""
	provider.Model = ""
	configured.Providers[configured.DefaultProvider] = provider

	if err := Validate(configured); err != nil {
		t.Fatalf("credentials and model should be optional during structural validation: %v", err)
	}
}

func TestValidateReportsStableFieldPaths(t *testing.T) {
	configured := Default()
	configured.Version = 2
	configured.DefaultProvider = "missing"
	configured.Providers["broken"] = ProviderConfig{
		API:             "invalid",
		Dialect:         "invalid",
		BaseURL:         "ftp://example.invalid/v1",
		MaxRetries:      -1,
		Temperature:     3,
		MaxOutputTokens: 0,
	}
	configured.Agent = AgentConfig{}
	configured.Logging.Level = "invalid"

	err := Validate(configured)
	if err == nil {
		t.Fatal("expected validation error")
	}

	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("unexpected error type: %T", err)
	}

	expectedPaths := []string{
		"version",
		"default_provider",
		"providers.broken.api",
		"providers.broken.dialect",
		"providers.broken.base_url",
		"providers.broken.timeout",
		"providers.broken.max_retries",
		"providers.broken.temperature",
		"providers.broken.max_output_tokens",
		"agent.max_iterations",
		"agent.max_tool_calls",
		"agent.max_input_tokens",
		"agent.max_output_tokens",
		"agent.max_duration",
		"agent.max_parallel_tools",
		"logging.level",
	}
	for _, path := range expectedPaths {
		if !strings.Contains(err.Error(), path+":") {
			t.Fatalf("validation error does not contain %q: %v", path, err)
		}
	}
}

func TestValidateRejectsAgentBudgetAboveLimits(t *testing.T) {
	configured := Default()
	configured.Agent = AgentConfig{
		MaxIterations:    maxAgentIterations + 1,
		MaxToolCalls:     maxAgentToolCalls + 1,
		MaxInputTokens:   maxAgentTokens + 1,
		MaxOutputTokens:  maxAgentTokens + 1,
		MaxDuration:      maxAgentDuration + 1,
		MaxParallelTools: maxParallelTools + 1,
	}

	err := Validate(configured)
	if err == nil {
		t.Fatal("expected oversized Agent budget to fail")
	}
	for _, path := range []string{
		"agent.max_iterations", "agent.max_tool_calls", "agent.max_input_tokens",
		"agent.max_output_tokens", "agent.max_duration", "agent.max_parallel_tools",
	} {
		if !strings.Contains(err.Error(), path+":") {
			t.Fatalf("validation error does not contain %q: %v", path, err)
		}
	}
}

func TestValidateRejectsInvalidLSPConfig(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*Config)
		expectPath string
	}{
		{
			name: "enabled without command",
			configure: func(configured *Config) {
				configured.LSP.Enabled = true
				configured.LSP.Command = ""
			},
			expectPath: "lsp.command",
		},
		{
			name: "invalid extension",
			configure: func(configured *Config) {
				configured.LSP.Extensions = []string{"go"}
			},
			expectPath: "lsp.extensions[0]",
		},
		{
			name: "duplicate extension",
			configure: func(configured *Config) {
				configured.LSP.Extensions = []string{".go", ".go"}
			},
			expectPath: "lsp.extensions[1]",
		},
		{
			name: "invalid timeout",
			configure: func(configured *Config) {
				configured.LSP.Timeout = maxLSPTimeout + time.Second
			},
			expectPath: "lsp.timeout",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configured := Default()
			test.configure(&configured)

			err := Validate(configured)
			if err == nil {
				t.Fatal("expected invalid LSP config to fail")
			}
			if !strings.Contains(err.Error(), test.expectPath+":") {
				t.Fatalf("validation error does not contain %q: %v", test.expectPath, err)
			}
		})
	}
}

func TestValidateRejectsInvalidBaseURLs(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
	}{
		{name: "empty"},
		{name: "relative", baseURL: "/v1"},
		{name: "unsupported scheme", baseURL: "ftp://example.invalid/v1"},
		{name: "userinfo", baseURL: "https://user:pass@example.invalid/v1"},
		{name: "fragment", baseURL: "https://example.invalid/v1#fragment"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configured := Default()
			provider := configured.Providers[configured.DefaultProvider]
			provider.BaseURL = test.baseURL
			configured.Providers[configured.DefaultProvider] = provider

			if err := Validate(configured); err == nil {
				t.Fatalf("expected invalid base URL %q to fail", test.baseURL)
			}
		})
	}
}

func TestCustomProvidersReceiveOperationalDefaults(t *testing.T) {
	configured := Default()
	api := APIChatCompletions
	providerName := "custom"
	baseURL := "https://custom.example.invalid/v1"
	configured = ApplyOverrides(configured, Overrides{
		Provider: &providerName,
		API:      &api,
		BaseURL:  &baseURL,
	})

	provider := configured.Providers[providerName]
	if provider.Dialect != DialectStandard {
		t.Fatalf("custom provider did not receive the standard dialect: %#v", provider)
	}
	if provider.Timeout <= 0 || provider.MaxOutputTokens <= 0 {
		t.Fatalf("custom provider did not receive operational defaults: %#v", provider)
	}
	if err := Validate(configured); err != nil {
		t.Fatalf("validate custom provider defaults: %v", err)
	}
}
