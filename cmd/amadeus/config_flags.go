package main

import (
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	flagConfig        = "config"
	flagModelProvider = "model-provider"
	flagWireAPI       = "wire-api"
	flagDialect       = "dialect"
	flagBaseURL       = "base-url"
	flagModel         = "model"
)

type configFlags struct {
	configPath string
	modelProvider string
	wireAPI       string
	dialect    string
	baseURL    string
	model      string
}

func (flags *configFlags) bind(command *cobra.Command) {
	persistent := command.PersistentFlags()
	persistent.StringVar(&flags.configPath, flagConfig, "", "use an explicit configuration file")
	persistent.StringVar(&flags.modelProvider, flagModelProvider, "", "override the model provider for this process")
	persistent.StringVar(&flags.wireAPI, flagWireAPI, "", "override the provider wire API for this process")
	persistent.StringVar(&flags.dialect, flagDialect, "", "override the provider dialect for this process")
	persistent.StringVar(&flags.baseURL, flagBaseURL, "", "override the provider base URL for this process")
	persistent.StringVar(&flags.model, flagModel, "", "override the model for this process")
}

func (flags *configFlags) configFile(command *cobra.Command) (string, bool) {
	flagSet := command.Root().PersistentFlags()
	return flags.configPath, flagSet.Changed(flagConfig)
}

func (flags *configFlags) apply(command *cobra.Command, configured config.Config) config.Config {
	flagSet := command.Root().PersistentFlags()
	overrides := config.Overrides{
		ModelProvider: optionalString(flagSet, flagModelProvider, flags.modelProvider),
		BaseURL:      optionalString(flagSet, flagBaseURL, flags.baseURL),
		Model:        optionalString(flagSet, flagModel, flags.model),
	}
	if flagSet.Changed(flagWireAPI) {
		wireAPI := config.WireAPI(flags.wireAPI)
		overrides.WireAPI = &wireAPI
	}
	if flagSet.Changed(flagDialect) {
		dialect := config.ProviderDialect(flags.dialect)
		overrides.Dialect = &dialect
	}

	return config.ApplyOverrides(configured, overrides)
}

func (flags *configFlags) sources(command *cobra.Command, configured config.Config) config.Sources {
	flagSet := command.Root().PersistentFlags()
	sources := make(config.Sources)
	provider := configured.ModelProvider
	if flagSet.Changed(flagModelProvider) {
		provider = flags.modelProvider
		sources["model_provider"] = config.Source{Kind: config.SourceCLI, Detail: "--" + flagModelProvider}
	}
	if flagSet.Changed(flagModel) {
		sources["model"] = config.Source{Kind: config.SourceCLI, Detail: "--" + flagModel}
	}
	for name, field := range map[string]string{
		flagWireAPI: "wire_api",
		flagDialect: "dialect",
		flagBaseURL: "base_url",
	} {
		if flagSet.Changed(name) {
			sources["model_providers."+provider+"."+field] = config.Source{Kind: config.SourceCLI, Detail: "--" + name}
		}
	}

	return sources
}

func optionalString(flagSet *pflag.FlagSet, name, value string) *string {
	if !flagSet.Changed(name) {
		return nil
	}

	return &value
}
