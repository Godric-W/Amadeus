package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCommand(flags *configFlags, runtime commandRuntime) *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Inspect and validate Amadeus configuration",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newConfigCheckCommand(flags, runtime))
	command.AddCommand(newConfigExplainCommand(flags, runtime))

	return command
}

func newConfigExplainCommand(flags *configFlags, runtime commandRuntime) *cobra.Command {
	return &cobra.Command{
		Use:   "explain",
		Short: "Print the effective configuration and each field source",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, path, err := loadConfiguredConfig(command, flags, runtime)
			if err != nil {
				return err
			}

			configured := flags.apply(command, loaded)
			if err := config.Validate(configured); err != nil {
				return err
			}

			fileSources, err := config.InspectYAMLSources(path)
			if err != nil {
				return err
			}
			lookupEnv := runtime.lookupEnv
			if lookupEnv == nil {
				lookupEnv = config.EnvLookup(func(string) (string, bool) { return "", false })
			}
			sources := config.MergeSources(
				config.SourcesFor(configured),
				fileSources,
				config.EnvironmentOverrideSources(loaded, lookupEnv),
				flags.sources(command, loaded),
			)

			writeConfigExplanation(command.OutOrStdout(), path, config.Redact(configured), sources)
			return nil
		},
	}
}

func newConfigCheckCommand(flags *configFlags, runtime commandRuntime) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Load and validate the effective configuration",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			configured, path, err := loadEffectiveConfig(command, flags, runtime)
			if err != nil {
				return err
			}
			if err := config.Validate(configured); err != nil {
				return err
			}

			fmt.Fprintln(command.OutOrStdout(), "configuration is valid")
			fmt.Fprintf(command.OutOrStdout(), "config: %s\n", path)
			fmt.Fprintf(command.OutOrStdout(), "provider: %s\n", configured.DefaultProvider)
			return nil
		},
	}
}

func loadEffectiveConfig(command *cobra.Command, flags *configFlags, runtime commandRuntime) (config.Config, string, error) {
	configured, path, err := loadConfiguredConfig(command, flags, runtime)
	if err != nil {
		return config.Config{}, path, err
	}
	return flags.apply(command, configured), path, nil
}

func loadConfiguredConfig(command *cobra.Command, flags *configFlags, runtime commandRuntime) (config.Config, string, error) {
	lookupEnv := runtime.lookupEnv
	if lookupEnv == nil {
		lookupEnv = config.EnvLookup(func(string) (string, bool) { return "", false })
	}

	var loader config.Loader
	if path, explicit := flags.configFile(command); explicit {
		loader = config.NewFileLoader(path)
	} else {
		if runtime.rootErr != nil {
			return config.Config{}, "", fmt.Errorf("resolve Amadeus root: %w", runtime.rootErr)
		}
		if strings.TrimSpace(runtime.amadeusRoot) == "" {
			return config.Config{}, "", errors.New("Amadeus root directory is empty")
		}
		loader = config.NewLoader(runtime.amadeusRoot)
	}

	loader = loader.WithEnvLookup(lookupEnv)
	configured, err := loader.Load()
	if err != nil {
		return config.Config{}, loader.ConfigPath(), err
	}

	return configured, loader.ConfigPath(), nil
}

func writeConfigExplanation(writer io.Writer, path string, configured config.Config, sources config.Sources) {
	fmt.Fprintf(writer, "config: %s\n", path)
	writeExplainedValue(writer, "version", configured.Version, sources)
	writeExplainedValue(writer, "default_provider", configured.DefaultProvider, sources)

	providerNames := make([]string, 0, len(configured.Providers))
	for name := range configured.Providers {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)
	for _, name := range providerNames {
		provider := configured.Providers[name]
		prefix := "providers." + name + "."
		writeExplainedValue(writer, prefix+"api", provider.API, sources)
		writeExplainedValue(writer, prefix+"dialect", provider.Dialect, sources)
		writeExplainedValue(writer, prefix+"api_key", provider.APIKey, sources)
		writeExplainedValue(writer, prefix+"base_url", provider.BaseURL, sources)
		writeExplainedValue(writer, prefix+"model", provider.Model, sources)
		writeExplainedValue(writer, prefix+"timeout", provider.Timeout, sources)
		writeExplainedValue(writer, prefix+"max_retries", provider.MaxRetries, sources)
		writeExplainedValue(writer, prefix+"temperature", provider.Temperature, sources)
		writeExplainedValue(writer, prefix+"max_output_tokens", provider.MaxOutputTokens, sources)
		writeExplainedValue(writer, prefix+"context_window", provider.ContextWindow, sources)
	}

	writeExplainedValue(writer, "agent.max_iterations", configured.Agent.MaxIterations, sources)
	writeExplainedValue(writer, "agent.max_tool_calls", configured.Agent.MaxToolCalls, sources)
	writeExplainedValue(writer, "agent.max_input_tokens", configured.Agent.MaxInputTokens, sources)
	writeExplainedValue(writer, "agent.max_output_tokens", configured.Agent.MaxOutputTokens, sources)
	writeExplainedValue(writer, "agent.max_duration", configured.Agent.MaxDuration, sources)
	writeExplainedValue(writer, "agent.max_parallel_tools", configured.Agent.MaxParallelTools, sources)
	writeExplainedValue(writer, "lsp.enabled", configured.LSP.Enabled, sources)
	writeExplainedValue(writer, "lsp.command", configured.LSP.Command, sources)
	writeExplainedValue(writer, "lsp.args", configured.LSP.Args, sources)
	writeExplainedValue(writer, "lsp.extensions", configured.LSP.Extensions, sources)
	writeExplainedValue(writer, "lsp.timeout", configured.LSP.Timeout, sources)
	writeExplainedValue(writer, "logging.level", configured.Logging.Level, sources)
	writeExplainedValue(writer, "logging.trace_llm", configured.Logging.TraceLLM, sources)
}

func writeExplainedValue(writer io.Writer, path string, value any, sources config.Sources) {
	source, ok := sources[path]
	if !ok {
		source = config.Source{Kind: config.SourceDefault}
	}
	fmt.Fprintf(writer, "%s: %v [source: %s]\n", path, value, source)
}
