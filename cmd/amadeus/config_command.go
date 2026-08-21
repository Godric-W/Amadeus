package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

func newConfigCommand(flags *configFlags, runtime commandRuntime) *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Inspect and validate Amadeus configuration",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newConfigCheckCommand(flags, runtime))
	command.AddCommand(newConfigShowCommand(flags, runtime))
	command.AddCommand(newConfigExplainCommand(flags, runtime))

	return command
}

func newConfigShowCommand(flags *configFlags, runtime commandRuntime) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the effective redacted configuration",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			configured, _, err := loadEffectiveConfig(command, flags, runtime)
			if err != nil {
				return err
			}
			if err := config.Validate(configured); err != nil {
				return err
			}
			encoder := yaml.NewEncoder(command.OutOrStdout())
			encoder.SetIndent(2)
			defer encoder.Close()
			return encoder.Encode(config.Redact(configured))
		},
	}
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
			fmt.Fprintf(command.OutOrStdout(), "model provider: %s\n", configured.ModelProvider)
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
	writeExplainedValue(writer, "model", configured.Model, sources)
	writeExplainedValue(writer, "model_provider", configured.ModelProvider, sources)
	writeExplainedValue(writer, "model_context_window", configured.ModelContextWindow, sources)
	writeExplainedValue(writer, "model_reasoning_effort", displayReasoningEffort(configured.ModelReasoningEffort), sources)
	writeExplainedValue(writer, "model_input_modalities", configured.ModelInputModalities, sources)
	writeExplainedValue(writer, "model_supports_original_image_detail", configured.ModelSupportsOriginalImageDetail, sources)
	writeExplainedValue(writer, "model_auto_compact_token_limit", configured.ModelAutoCompactTokenLimit, sources)
	writeExplainedValue(writer, "tool_output_token_limit", configured.ToolOutputTokenLimit, sources)

	providerNames := make([]string, 0, len(configured.ModelProviders))
	for name := range configured.ModelProviders {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)
	for _, name := range providerNames {
		provider := configured.ModelProviders[name]
		prefix := "model_providers." + name + "."
		writeExplainedValue(writer, prefix+"wire_api", provider.WireAPI, sources)
		writeExplainedValue(writer, prefix+"dialect", provider.Dialect, sources)
		writeExplainedValue(writer, prefix+"api_key", provider.APIKey, sources)
		writeExplainedValue(writer, prefix+"base_url", provider.BaseURL, sources)
		writeExplainedValue(writer, prefix+"timeout", provider.Timeout, sources)
		writeExplainedValue(writer, prefix+"request_max_retries", provider.RequestMaxRetries, sources)
		writeExplainedValue(writer, prefix+"stream_max_retries", provider.StreamMaxRetries, sources)
		writeExplainedValue(writer, prefix+"stream_idle_timeout", provider.StreamIdleTimeout, sources)
	}

	writeExplainedValue(writer, "agent.max_parallel_tools", configured.Agent.MaxParallelTools, sources)
	writeExplainedValue(writer, "web.fetch.enabled", configured.Web.Fetch.Enabled, sources)
	writeExplainedValue(writer, "web.fetch.timeout", configured.Web.Fetch.Timeout, sources)
	writeExplainedValue(writer, "web.fetch.max_bytes", configured.Web.Fetch.MaxBytes, sources)
	writeExplainedValue(writer, "web.fetch.max_redirects", configured.Web.Fetch.MaxRedirects, sources)
	writeExplainedValue(writer, "web.search.enabled", configured.Web.Search.Enabled, sources)
	writeExplainedValue(writer, "web.search.provider", configured.Web.Search.Provider, sources)
	writeExplainedValue(writer, "web.search.api_key", configured.Web.Search.APIKey, sources)
	writeExplainedValue(writer, "web.search.base_url", configured.Web.Search.BaseURL, sources)
	writeExplainedValue(writer, "web.search.timeout", configured.Web.Search.Timeout, sources)
	writeExplainedValue(writer, "web.search.max_results", configured.Web.Search.MaxResults, sources)
	writeExplainedValue(writer, "logging.level", configured.Logging.Level, sources)
	writeExplainedValue(writer, "logging.trace_llm", configured.Logging.TraceLLM, sources)
}

func displayReasoningEffort(effort *llm.ReasoningEffort) string {
	if effort == nil {
		return "provider default (unset)"
	}
	return string(*effort)
}

func writeExplainedValue(writer io.Writer, path string, value any, sources config.Sources) {
	source, ok := sources[path]
	if !ok {
		source = config.Source{Kind: config.SourceDefault}
	}
	fmt.Fprintf(writer, "%s: %v [source: %s]\n", path, value, source)
}
