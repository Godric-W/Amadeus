package cli

import (
	"fmt"

	"github.com/Godric-W/Amadeus/internal/bootstrap"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

func newConfigCommand(flags *configFlags, environment bootstrap.Environment) *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Inspect and validate Amadeus configuration",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newConfigCheckCommand(flags, environment))
	command.AddCommand(newConfigShowCommand(flags, environment))
	command.AddCommand(newConfigExplainCommand(flags, environment))

	return command
}

func newConfigShowCommand(flags *configFlags, environment bootstrap.Environment) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the effective redacted configuration",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			configured, _, err := loadEffectiveConfig(command, flags, environment)
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

func newConfigExplainCommand(flags *configFlags, environment bootstrap.Environment) *cobra.Command {
	return &cobra.Command{
		Use:   "explain",
		Short: "Print the effective configuration and each field source",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, path, err := loadConfiguredConfig(command, flags, environment)
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
			lookupEnv := environment.LookupEnv
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

func newConfigCheckCommand(flags *configFlags, environment bootstrap.Environment) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Load and validate the effective configuration",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			configured, path, err := loadEffectiveConfig(command, flags, environment)
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
