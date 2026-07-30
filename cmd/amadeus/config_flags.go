package main

import (
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	flagConfig   = "config"
	flagProvider = "provider"
	flagAPI      = "api"
	flagDialect  = "dialect"
	flagBaseURL  = "base-url"
	flagModel    = "model"
)

type configFlags struct {
	configPath string
	provider   string
	api        string
	dialect    string
	baseURL    string
	model      string
}

func (flags *configFlags) bind(command *cobra.Command) {
	persistent := command.PersistentFlags()
	persistent.StringVar(&flags.configPath, flagConfig, "", "use an explicit configuration file")
	persistent.StringVar(&flags.provider, flagProvider, "", "override the default provider for this process")
	persistent.StringVar(&flags.api, flagAPI, "", "override the API mode for this process")
	persistent.StringVar(&flags.dialect, flagDialect, "", "override the provider dialect for this process")
	persistent.StringVar(&flags.baseURL, flagBaseURL, "", "override the provider base URL for this process")
	persistent.StringVar(&flags.model, flagModel, "", "override the provider model for this process")
}

func (flags *configFlags) configFile(command *cobra.Command) (string, bool) {
	flagSet := command.Root().PersistentFlags()
	return flags.configPath, flagSet.Changed(flagConfig)
}

func (flags *configFlags) apply(command *cobra.Command, configured config.Config) config.Config {
	flagSet := command.Root().PersistentFlags()
	overrides := config.Overrides{
		Provider: optionalString(flagSet, flagProvider, flags.provider),
		BaseURL:  optionalString(flagSet, flagBaseURL, flags.baseURL),
		Model:    optionalString(flagSet, flagModel, flags.model),
	}
	if flagSet.Changed(flagAPI) {
		api := config.APIMode(flags.api)
		overrides.API = &api
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
	provider := configured.DefaultProvider
	if flagSet.Changed(flagProvider) {
		provider = flags.provider
		sources["default_provider"] = config.Source{Kind: config.SourceCLI, Detail: "--" + flagProvider}
	}
	for name, field := range map[string]string{
		flagAPI:     "api",
		flagDialect: "dialect",
		flagBaseURL: "base_url",
		flagModel:   "model",
	} {
		if flagSet.Changed(name) {
			sources["providers."+provider+"."+field] = config.Source{Kind: config.SourceCLI, Detail: "--" + name}
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
