package config

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
)

func validConfig() Config {
	configured := Default()
	configured.Model = "test-model"
	configured.ModelProvider = "compatible"
	configured.ModelContextWindow = 128_000
	configured.ModelProviders["compatible"] = defaultModelProviderInfo()
	provider := configured.ModelProviders["compatible"]
	provider.BaseURL = "https://example.invalid/v1"
	configured.ModelProviders["compatible"] = provider
	return configured
}

func TestValidateAcceptsConfigV2(t *testing.T) {
	if err := Validate(validConfig()); err != nil {
		t.Fatalf("validate config: %v", err)
	}
}

func TestValidateRejectsInvalidMultiAgentLimits(t *testing.T) {
	configured := validConfig()
	configured.Agent.MultiAgent.MaxAgents = 0
	configured.Agent.MultiAgent.MaxDepth = 2
	configured.Agent.MultiAgent.ChildMaxDuration = 0
	err := Validate(configured)
	if err == nil || !strings.Contains(err.Error(), "agent.multi_agent.max_agents") || !strings.Contains(err.Error(), "agent.multi_agent.max_depth") || !strings.Contains(err.Error(), "agent.multi_agent.child_max_duration") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateReportsStableModelAndProviderPaths(t *testing.T) {
	configured := validConfig()
	configured.Version = 1
	configured.Model = ""
	configured.ModelProvider = "missing"
	configured.ModelContextWindow = 0
	configured.ModelAutoCompactTokenLimit = 1
	configured.ToolOutputTokenLimit = 0
	configured.ModelProviders["broken"] = ModelProviderInfo{
		WireAPI:           "unknown",
		Dialect:           "unknown",
		BaseURL:           "relative",
		Timeout:           0,
		RequestMaxRetries: -1,
		StreamMaxRetries:  101,
		StreamIdleTimeout: 0,
	}
	err := Validate(configured)
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("expected validation error: %v", err)
	}
	message := validationError.Error()
	for _, path := range []string{
		"version", "model", "model_provider", "model_context_window", "model_auto_compact_token_limit",
		"tool_output_token_limit", "model_providers.broken.wire_api", "model_providers.broken.dialect",
		"model_providers.broken.base_url", "model_providers.broken.timeout",
		"model_providers.broken.request_max_retries", "model_providers.broken.stream_max_retries",
		"model_providers.broken.stream_idle_timeout",
	} {
		if !strings.Contains(message, path) {
			t.Errorf("validation error missing path %q: %s", path, message)
		}
	}
}

func TestValidateRejectsInvalidModelImageCapabilities(t *testing.T) {
	tests := []struct {
		name       string
		modalities []llm.InputModality
		original   bool
		path       string
	}{
		{name: "missing text", modalities: []llm.InputModality{llm.InputModalityImage}, path: "model_input_modalities"},
		{name: "duplicate", modalities: []llm.InputModality{llm.InputModalityText, llm.InputModalityText}, path: "model_input_modalities[1]"},
		{name: "unknown", modalities: []llm.InputModality{llm.InputModalityText, "audio"}, path: "model_input_modalities[1]"},
		{name: "original without image", modalities: []llm.InputModality{llm.InputModalityText}, original: true, path: "model_supports_original_image_detail"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configured := validConfig()
			configured.ModelInputModalities = test.modalities
			configured.ModelSupportsOriginalImageDetail = test.original
			if err := Validate(configured); err == nil || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("expected %s validation error: %v", test.path, err)
			}
		})
	}
}

func TestValidateAutoCompactLimitUsesNinetyPercentCeiling(t *testing.T) {
	configured := validConfig()
	configured.ModelContextWindow = 100_000
	configured.ModelAutoCompactTokenLimit = 90_000
	if err := Validate(configured); err != nil {
		t.Fatalf("90%% compact limit should validate: %v", err)
	}
	configured.ModelAutoCompactTokenLimit = 90_001
	if err := Validate(configured); err == nil || !strings.Contains(err.Error(), "model_auto_compact_token_limit") {
		t.Fatalf("expected compact limit error: %v", err)
	}
}

func TestValidateRejectsInvalidReasoningEffort(t *testing.T) {
	configured := validConfig()
	effort := llm.ReasoningEffort("maximum")
	configured.ModelReasoningEffort = &effort
	if err := Validate(configured); err == nil || !strings.Contains(err.Error(), "model_reasoning_effort") {
		t.Fatalf("expected reasoning effort error: %v", err)
	}
}

func TestValidateAllowsRetryDisableWithExplicitZero(t *testing.T) {
	configured := validConfig()
	provider := configured.ModelProviders[configured.ModelProvider]
	provider.RequestMaxRetries = 0
	provider.StreamMaxRetries = 0
	configured.ModelProviders[configured.ModelProvider] = provider
	if err := Validate(configured); err != nil {
		t.Fatalf("retry disable should validate: %v", err)
	}
}

func TestValidateRejectsInvalidProviderURL(t *testing.T) {
	configured := validConfig()
	provider := configured.ModelProviders[configured.ModelProvider]
	provider.BaseURL = "ftp://user@example.invalid/v1#fragment"
	configured.ModelProviders[configured.ModelProvider] = provider
	err := Validate(configured)
	if err == nil || !strings.Contains(err.Error(), "model_providers.compatible.base_url") {
		t.Fatalf("expected provider URL error: %v", err)
	}
}

func TestValidateRejectsAgentParallelismAboveLimit(t *testing.T) {
	configured := validConfig()
	configured.Agent.MaxParallelTools = 65
	if err := Validate(configured); err == nil || !strings.Contains(err.Error(), "agent.max_parallel_tools") {
		t.Fatalf("expected parallelism error: %v", err)
	}
}

func TestCustomProviderReceivesOperationalDefaults(t *testing.T) {
	configured := Default()
	patch := configPatch{
		Model:              stringPointer("model"),
		ModelProvider:      stringPointer("custom"),
		ModelContextWindow: int64Pointer(128_000),
		ModelProviders: map[string]modelProviderPatch{
			"custom": {BaseURL: stringPointer("https://example.invalid/v1")},
		},
	}
	configured = patch.apply(configured)
	provider := configured.ModelProviders["custom"]
	if provider.WireAPI != WireAPIResponses || provider.Dialect != DialectStandard || provider.Timeout != 2*time.Minute {
		t.Fatalf("unexpected provider defaults: %#v", provider)
	}
	if provider.RequestMaxRetries != 4 || provider.StreamMaxRetries != 5 || provider.StreamIdleTimeout != 5*time.Minute {
		t.Fatalf("unexpected retry defaults: %#v", provider)
	}
}

func stringPointer(value string) *string { return &value }
func int64Pointer(value int64) *int64    { return &value }
