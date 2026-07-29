package main

import (
	"bytes"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
)

func TestConfigFlagsOverrideEnvironmentConfiguration(t *testing.T) {
	flags := &configFlags{}
	command := newRootCommandWithConfigFlags(flags)
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{
		"version",
		"--provider", "cli",
		"--api", string(config.APIChatCompletions),
		"--base-url", "https://cli.example.invalid/v1",
		"--model", "cli-model",
	})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute command with config flags: %v", err)
	}

	environmentProvider := "environment"
	environmentAPI := config.APIResponses
	environmentBaseURL := "https://environment.example.invalid/v1"
	environmentModel := "environment-model"
	configured := config.ApplyOverrides(config.Default(), config.Overrides{
		Provider: &environmentProvider,
		API:      &environmentAPI,
		BaseURL:  &environmentBaseURL,
		Model:    &environmentModel,
	})

	configured = flags.apply(command, configured)
	if configured.DefaultProvider != "cli" {
		t.Fatalf("CLI provider did not win: got %q", configured.DefaultProvider)
	}
	provider := configured.Providers["cli"]
	if provider.API != config.APIChatCompletions {
		t.Fatalf("CLI API mode did not win: got %q", provider.API)
	}
	if provider.BaseURL != "https://cli.example.invalid/v1" {
		t.Fatalf("CLI base URL did not win: got %q", provider.BaseURL)
	}
	if provider.Model != "cli-model" {
		t.Fatalf("CLI model did not win: got %q", provider.Model)
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
	provider := configured.Providers[configured.DefaultProvider]
	provider.Model = "environment-model"
	configured.Providers[configured.DefaultProvider] = provider

	configured = flags.apply(command, configured)
	if configured.Providers[configured.DefaultProvider].Model != "" {
		t.Fatalf("explicit empty model did not override configuration: %#v", configured.Providers[configured.DefaultProvider])
	}
}

func TestRootCommandDoesNotExposeAPIKeyFlag(t *testing.T) {
	command := newRootCommand()
	if command.PersistentFlags().Lookup("api-key") != nil {
		t.Fatal("root command must not expose an API key flag")
	}
}
