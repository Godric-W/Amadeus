package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/buildinfo"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/spf13/cobra"
)

const envAmadeusHome = "AMADEUS_HOME"

type commandRuntime struct {
	amadeusRoot        string
	rootErr            error
	lookupEnv          config.EnvLookup
	llmClientFactory   llmClientFactory
	turnContextFactory chatTurnContextFactory
}

func newRootCommand() *cobra.Command {
	return newRootCommandWithRuntime(&configFlags{}, defaultCommandRuntime())
}

func newRootCommandWithConfigFlags(flags *configFlags) *cobra.Command {
	return newRootCommandWithRuntime(flags, defaultCommandRuntime())
}

func newRootCommandWithRuntime(flags *configFlags, runtime commandRuntime) *cobra.Command {
	command := &cobra.Command{
		Use:           "amadeus",
		Short:         "Amadeus agent CLI",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	flags.bind(command)
	command.AddCommand(newChatCommand(flags, runtime))
	command.AddCommand(newConfigCommand(flags, runtime))
	command.AddCommand(newVersionCommand(buildinfo.Current()))

	return command
}

func defaultCommandRuntime() commandRuntime {
	lookupEnv := config.EnvLookup(os.LookupEnv)
	amadeusRoot, err := resolveAmadeusRoot(lookupEnv, os.Executable, filepath.EvalSymlinks)
	return commandRuntime{
		amadeusRoot: amadeusRoot,
		rootErr:     err,
		lookupEnv:   lookupEnv,
	}
}

func resolveAmadeusRoot(
	lookupEnv config.EnvLookup,
	executable func() (string, error),
	evalSymlinks func(string) (string, error),
) (string, error) {
	if amadeusRoot, ok := lookupEnv(envAmadeusHome); ok && strings.TrimSpace(amadeusRoot) != "" {
		return amadeusRoot, nil
	}

	executablePath, err := executable()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(executablePath) == "" {
		return "", errors.New("executable path is empty")
	}
	if resolvedPath, resolveErr := evalSymlinks(executablePath); resolveErr == nil {
		executablePath = resolvedPath
	}

	return filepath.Dir(executablePath), nil
}
