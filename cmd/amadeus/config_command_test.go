package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/spf13/cobra"
)

func TestConfigCheckRejectsMissingUserModelProvider(t *testing.T) {
	command, output := newTestRootCommand(t.TempDir())
	command.SetArgs([]string{"config", "check"})

	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "model_provider") || !strings.Contains(err.Error(), "model_providers") {
		t.Fatalf("missing user Provider was not rejected: err=%v output=%q", err, output.String())
	}
}

func TestConfigCheckRejectsInvalidConfiguration(t *testing.T) {
	amadeusRoot := t.TempDir()
	writeCommandConfig(t, filepath.Join(amadeusRoot, "config.yaml"), `
model: test-model
model_provider: openai
model_context_window: 8192
model_providers:
  openai:
    wire_api: responses
    dialect: openai
    base_url: not-a-url
`)
	command, _ := newTestRootCommand(amadeusRoot)
	command.SetArgs([]string{"config", "check"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected invalid configuration error")
	}
	if !strings.Contains(err.Error(), "model_providers.openai.base_url") {
		t.Fatalf("validation error does not contain field path: %v", err)
	}
}

func TestConfigCheckAppliesCLIFlagsBeforeValidation(t *testing.T) {
	amadeusRoot := t.TempDir()
	writeCommandConfig(t, filepath.Join(amadeusRoot, "config.yaml"), `
model: test-model
model_provider: openai
model_context_window: 8192
model_providers:
  openai:
    wire_api: invalid
    dialect: openai
    base_url: https://example.invalid/v1
`)
	command, output := newTestRootCommand(amadeusRoot)
	command.SetArgs([]string{"config", "check", "--wire-api", string(config.WireAPIResponses)})

	if err := command.Execute(); err != nil {
		t.Fatalf("CLI flag did not repair file configuration before validation: %v", err)
	}
	if !strings.Contains(output.String(), "configuration is valid") {
		t.Fatalf("unexpected check output: %q", output.String())
	}
}

func TestConfigCheckUsesExplicitConfigWithoutAmadeusRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "explicit.yaml")
	writeCommandConfig(t, path, `
model: explicit-model
model_provider: openai
model_context_window: 8192
model_providers:
  openai:
    wire_api: responses
    dialect: openai
    base_url: https://example.invalid/v1
`)

	var output bytes.Buffer
	flags := &configFlags{}
	command := newRootCommandWithRuntime(flags, commandRuntime{
		rootErr:   errors.New("root unavailable"),
		lookupEnv: emptyEnvLookup,
	})
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"config", "check", "--config", path})

	if err := command.Execute(); err != nil {
		t.Fatalf("check explicit config without Amadeus root: %v", err)
	}
	if !strings.Contains(output.String(), "config: "+path) {
		t.Fatalf("check output does not contain explicit path: %q", output.String())
	}
}

func TestConfigCheckDoesNotPrintAPIKey(t *testing.T) {
	amadeusRoot := t.TempDir()
	const secret = "do-not-print-this-secret"
	writeCommandConfig(t, filepath.Join(amadeusRoot, "config.yaml"), `
model: test-model
model_provider: openai
model_context_window: 8192
model_providers:
  openai:
    wire_api: responses
    dialect: openai
    api_key: do-not-print-this-secret
    base_url: https://example.invalid/v1
`)
	command, output := newTestRootCommand(amadeusRoot)
	command.SetArgs([]string{"config", "check"})

	if err := command.Execute(); err != nil {
		t.Fatalf("check config containing API key: %v", err)
	}
	if strings.Contains(output.String(), secret) {
		t.Fatalf("config check output leaked API key: %q", output.String())
	}
}

func TestConfigExplainShowsFinalValuesAndSources(t *testing.T) {
	amadeusRoot := t.TempDir()
	path := filepath.Join(amadeusRoot, "config.yaml")
	const secret = "explain-must-not-print-this"
	writeCommandConfig(t, path, `
model: file-model
model_provider: compatible
model_context_window: 128000
model_providers:
  compatible:
    wire_api: chat_completions
    dialect: deepseek
    api_key: ${FILE_API_KEY}
    base_url: https://file.example.invalid/v1
`)

	lookupEnv := config.EnvLookup(func(name string) (string, bool) {
		values := map[string]string{
			"FILE_API_KEY":  secret,
			config.EnvModel: "environment-model",
		}
		value, ok := values[name]
		return value, ok
	})
	var output bytes.Buffer
	command := newRootCommandWithRuntime(&configFlags{}, commandRuntime{
		amadeusRoot: amadeusRoot,
		lookupEnv:   lookupEnv,
	})
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"config", "explain",
		"--dialect", string(config.DialectQwen),
		"--base-url", "https://cli.example.invalid/v1",
		"--model-reasoning-effort", "xhigh",
	})

	if err := command.Execute(); err != nil {
		t.Fatalf("explain effective config: %v", err)
	}

	explanation := output.String()
	expected := []string{
		"model_provider: compatible [source: file: " + path + "]",
		"model: environment-model [source: environment: " + config.EnvModel + "]",
		"model_reasoning_effort: xhigh [source: cli: --model-reasoning-effort]",
		"model_providers.compatible.dialect: qwen [source: cli: --dialect]",
		"model_providers.compatible.api_key: " + config.RedactedSecret + " [source: environment: FILE_API_KEY via " + path + "]",
		"model_providers.compatible.base_url: https://cli.example.invalid/v1 [source: cli: --base-url]",
		"model_providers.compatible.timeout: 2m0s [source: default]",
		"agent.max_parallel_tools: 4 [source: default]",
	}
	for _, value := range expected {
		if !strings.Contains(explanation, value) {
			t.Fatalf("explanation does not contain %q:\n%s", value, explanation)
		}
	}
	if strings.Contains(explanation, "agent.mode") {
		t.Fatalf("config explanation contains removed agent mode:\n%s", explanation)
	}
	if strings.Contains(explanation, "\nversion:") {
		t.Fatalf("config explanation contains removed schema version:\n%s", explanation)
	}
	if strings.Contains(explanation, secret) {
		t.Fatalf("config explanation leaked API key: %s", explanation)
	}
}

func TestConfigShowPrintsRedactedEffectiveConfiguration(t *testing.T) {
	amadeusRoot := t.TempDir()
	writeCommandConfig(t, filepath.Join(amadeusRoot, "config.yaml"), `
model: test-model
model_provider: openai
model_context_window: 8192
model_reasoning_effort: high
model_providers:
  openai:
    wire_api: responses
    dialect: openai
    api_key: secret-value
    base_url: https://example.invalid/v1
`)
	command, output := newTestRootCommand(amadeusRoot)
	command.SetArgs([]string{"config", "show"})
	if err := command.Execute(); err != nil {
		t.Fatalf("show config: %v", err)
	}
	shown := output.String()
	if !strings.Contains(shown, "model_reasoning_effort: high") || !strings.Contains(shown, config.RedactedSecret) {
		t.Fatalf("unexpected shown config:\n%s", shown)
	}
	if strings.Contains(shown, "secret-value") {
		t.Fatalf("config show leaked API key: %s", shown)
	}
	if strings.Contains(shown, "\nversion:") || strings.HasPrefix(shown, "version:") {
		t.Fatalf("config show contains removed schema version: %s", shown)
	}
}

func TestConfigExplainRejectsInvalidEffectiveConfiguration(t *testing.T) {
	amadeusRoot := t.TempDir()
	writeCommandConfig(t, filepath.Join(amadeusRoot, "config.yaml"), `
model: test-model
model_provider: openai
model_context_window: 8192
model_providers:
  openai:
    wire_api: invalid
    dialect: openai
    base_url: https://example.invalid/v1
`)
	command, _ := newTestRootCommand(amadeusRoot)
	command.SetArgs([]string{"config", "explain"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected invalid explanation configuration error")
	}
	if !strings.Contains(err.Error(), "model_providers.openai.wire_api") {
		t.Fatalf("explain validation error does not contain field path: %v", err)
	}
}

func newTestRootCommand(amadeusRoot string) (*cobra.Command, *bytes.Buffer) {
	var output bytes.Buffer
	command := newRootCommandWithRuntime(&configFlags{}, commandRuntime{
		amadeusRoot: amadeusRoot,
		lookupEnv:   emptyEnvLookup,
	})
	command.SetOut(&output)
	command.SetErr(&output)
	return command, &output
}

func emptyEnvLookup(string) (string, bool) {
	return "", false
}

func writeCommandConfig(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
}
