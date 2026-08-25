package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/bootstrap"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/spf13/cobra"
)

func loadEffectiveConfig(command *cobra.Command, flags *configFlags, environment bootstrap.Environment) (config.Config, string, error) {
	configured, path, err := loadConfiguredConfig(command, flags, environment)
	if err != nil {
		return config.Config{}, path, err
	}
	return flags.apply(command, configured), path, nil
}

func loadConfiguredConfig(command *cobra.Command, flags *configFlags, environment bootstrap.Environment) (config.Config, string, error) {
	lookupEnv := environment.LookupEnv
	if lookupEnv == nil {
		lookupEnv = config.EnvLookup(func(string) (string, bool) { return "", false })
	}

	var loader config.Loader
	if path, explicit := flags.configFile(command); explicit {
		loader = config.NewFileLoader(path)
	} else {
		if environment.AmadeusRootError != nil {
			return config.Config{}, "", fmt.Errorf("resolve Amadeus root: %w", environment.AmadeusRootError)
		}
		if strings.TrimSpace(environment.AmadeusRoot) == "" {
			return config.Config{}, "", errors.New("Amadeus root directory is empty")
		}
		loader = config.NewLoader(environment.AmadeusRoot)
	}

	loader = loader.WithEnvLookup(lookupEnv)
	configured, err := loader.Load()
	if err != nil {
		return config.Config{}, loader.ConfigPath(), err
	}
	return configured, loader.ConfigPath(), nil
}
