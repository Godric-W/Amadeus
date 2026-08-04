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

func TestConfigCheckAcceptsDefaultConfiguration(t *testing.T) {
	command, output := newTestRootCommand(t.TempDir())
	command.SetArgs([]string{"config", "check"})

	if err := command.Execute(); err != nil {
		t.Fatalf("check default config: %v", err)
	}
	if !strings.Contains(output.String(), "configuration is valid") {
		t.Fatalf("unexpected check output: %q", output.String())
	}
	if !strings.Contains(output.String(), "provider: openai") {
		t.Fatalf("check output does not contain provider: %q", output.String())
	}
}

func TestConfigCheckRejectsInvalidConfiguration(t *testing.T) {
	amadeusRoot := t.TempDir()
	writeCommandConfig(t, filepath.Join(amadeusRoot, "config.yaml"), `
providers:
  openai:
    base_url: not-a-url
`)
	command, _ := newTestRootCommand(amadeusRoot)
	command.SetArgs([]string{"config", "check"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected invalid configuration error")
	}
	if !strings.Contains(err.Error(), "providers.openai.base_url") {
		t.Fatalf("validation error does not contain field path: %v", err)
	}
}

func TestConfigCheckAppliesCLIFlagsBeforeValidation(t *testing.T) {
	amadeusRoot := t.TempDir()
	writeCommandConfig(t, filepath.Join(amadeusRoot, "config.yaml"), `
providers:
  openai:
    api: invalid
`)
	command, output := newTestRootCommand(amadeusRoot)
	command.SetArgs([]string{"config", "check", "--api", string(config.APIResponses)})

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
providers:
  openai:
    model: explicit-model
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
providers:
  openai:
    api_key: do-not-print-this-secret
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
default_provider: compatible
providers:
  compatible:
    api: chat_completions
    dialect: deepseek
    api_key: ${FILE_API_KEY}
    base_url: https://file.example.invalid/v1
    model: file-model
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
	})

	if err := command.Execute(); err != nil {
		t.Fatalf("explain effective config: %v", err)
	}

	explanation := output.String()
	expected := []string{
		"default_provider: compatible [source: file: " + path + "]",
		"providers.compatible.dialect: qwen [source: cli: --dialect]",
		"providers.compatible.api_key: " + config.RedactedSecret + " [source: environment: FILE_API_KEY via " + path + "]",
		"providers.compatible.base_url: https://cli.example.invalid/v1 [source: cli: --base-url]",
		"providers.compatible.model: environment-model [source: environment: " + config.EnvModel + "]",
		"providers.compatible.timeout: 2m0s [source: default]",
		"agent.max_iterations: 30 [source: default]",
		"agent.max_tool_calls: 120 [source: default]",
		"agent.max_input_tokens: 1000000 [source: default]",
		"agent.max_output_tokens: 245760 [source: default]",
		"agent.max_duration: 30m0s [source: default]",
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
	if strings.Contains(explanation, secret) {
		t.Fatalf("config explanation leaked API key: %s", explanation)
	}
}

func TestConfigExplainRejectsInvalidEffectiveConfiguration(t *testing.T) {
	amadeusRoot := t.TempDir()
	writeCommandConfig(t, filepath.Join(amadeusRoot, "config.yaml"), `
providers:
  openai:
    api: invalid
`)
	command, _ := newTestRootCommand(amadeusRoot)
	command.SetArgs([]string{"config", "explain"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected invalid explanation configuration error")
	}
	if !strings.Contains(err.Error(), "providers.openai.api") {
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
