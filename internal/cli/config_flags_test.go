package cli

import (
	"bytes"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestConfigFlagsOverrideEnvironmentConfiguration(t *testing.T) {
	flags := &configFlags{}
	command := newRootCommandWithConfigFlags(flags)
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{
		"version",
		"--model-provider", "cli",
		"--wire-api", string(config.WireAPIChatCompletions),
		"--dialect", string(config.DialectGLM),
		"--base-url", "https://cli.example.invalid/v1",
		"--model", "cli-model",
		"--model-reasoning-effort", string(llm.ReasoningEffortHigh),
		"--model-input-modalities", "text,image",
		"--model-supports-original-image-detail",
	})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute command with config flags: %v", err)
	}

	environmentProvider := "environment"
	environmentWireAPI := config.WireAPIResponses
	environmentDialect := config.DialectOpenAI
	environmentBaseURL := "https://environment.example.invalid/v1"
	environmentModel := "environment-model"
	environmentReasoningEffort := llm.ReasoningEffortLow
	configured := config.ApplyOverrides(config.Default(), config.Overrides{
		ModelProvider:        &environmentProvider,
		WireAPI:              &environmentWireAPI,
		Dialect:              &environmentDialect,
		BaseURL:              &environmentBaseURL,
		Model:                &environmentModel,
		ModelReasoningEffort: &environmentReasoningEffort,
	})

	configured = flags.apply(command, configured)
	if configured.ModelProvider != "cli" {
		t.Fatalf("CLI provider did not win: got %q", configured.ModelProvider)
	}
	provider := configured.ModelProviders["cli"]
	if provider.WireAPI != config.WireAPIChatCompletions {
		t.Fatalf("CLI wire API did not win: got %q", provider.WireAPI)
	}
	if provider.Dialect != config.DialectGLM {
		t.Fatalf("CLI dialect did not win: got %q", provider.Dialect)
	}
	if provider.BaseURL != "https://cli.example.invalid/v1" {
		t.Fatalf("CLI base URL did not win: got %q", provider.BaseURL)
	}
	if configured.Model != "cli-model" {
		t.Fatalf("CLI model did not win: got %q", configured.Model)
	}
	if configured.ModelReasoningEffort == nil || *configured.ModelReasoningEffort != llm.ReasoningEffortHigh {
		t.Fatalf("CLI reasoning effort did not win: %#v", configured.ModelReasoningEffort)
	}
	if len(configured.ModelInputModalities) != 2 || configured.ModelInputModalities[1] != llm.InputModalityImage || !configured.ModelSupportsOriginalImageDetail {
		t.Fatalf("CLI model image capabilities did not win: %#v", configured)
	}
}

func TestConfigFlagSelectsExplicitFile(t *testing.T) {
	flags := &configFlags{}
	command := newRootCommandWithConfigFlags(flags)
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"version", "--config", "./configs/development.yaml"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute command with config path: %v", err)
	}

	path, ok := flags.configFile(command)
	if !ok {
		t.Fatal("explicit config flag was not marked as set")
	}
	if path != "./configs/development.yaml" {
		t.Fatalf("unexpected explicit config path: got %q", path)
	}
}

func TestConfigFlagsAllowExplicitEmptyValues(t *testing.T) {
	flags := &configFlags{}
	command := newRootCommandWithConfigFlags(flags)
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"version", "--model="})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute command with empty model flag: %v", err)
	}

	configured := config.Default()
	configured.Model = "environment-model"

	configured = flags.apply(command, configured)
	if configured.Model != "" {
		t.Fatalf("explicit empty model did not override configuration: %q", configured.Model)
	}
}

func TestRootCommandDoesNotExposeAPIKeyFlag(t *testing.T) {
	command := newRootCommand()
	if command.PersistentFlags().Lookup("api-key") != nil {
		t.Fatal("root command must not expose an API key flag")
	}
}
