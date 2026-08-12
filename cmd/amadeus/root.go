package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/buildinfo"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
	"github.com/spf13/cobra"
)

const envAmadeusHome = "AMADEUS_HOME"
const flagPlain = "plain"

type commandRuntime struct {
	amadeusRoot         string
	rootErr             error
	workingDirectory    string
	workingDirectoryErr error
	lookupEnv           config.EnvLookup
	llmClientFactory    llmClientFactory
	mcpClientFactory    mcp.ClientFactory
	webFetcher          webfetch.Fetcher
	webSearch           websearch.Provider
	agentContextFactory turnContextFactory
	agentCommand        agentCommand
	agentCommandFactory agentCommandFactory
	terminalDetector    terminalDetector
	auditSinkFactory    auditSinkFactory
	threadStoreFactory  threadStoreFactory
	persistentIDFactory func(string) string
	now                 func() time.Time
	turnIDFactory       func() string
}

func newRootCommand() *cobra.Command {
	return newRootCommandWithRuntime(&configFlags{}, defaultCommandRuntime())
}

func newRootCommandWithConfigFlags(flags *configFlags) *cobra.Command {
	return newRootCommandWithRuntime(flags, defaultCommandRuntime())
}

func newRootCommandWithRuntime(flags *configFlags, runtime commandRuntime) *cobra.Command {
	return newRootCommandWithFlags(flags, &projectFlags{}, runtime)
}

func newRootCommandWithFlags(configFlags *configFlags, projectFlags *projectFlags, runtime commandRuntime) *cobra.Command {
	sessionFlags := &sessionFlags{}
	plain := false
	command := &cobra.Command{
		Use:           "amadeus [task]",
		Short:         "Amadeus agent CLI",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args: func(command *cobra.Command, arguments []string) error {
			if command.Flags().Changed(flagResume) && sessionFlags.resume == resumeSelectorFlag {
				return cobra.MaximumNArgs(2)(command, arguments)
			}
			return cobra.MaximumNArgs(1)(command, arguments)
		},
		RunE: func(command *cobra.Command, arguments []string) error {
			if command.Flags().Changed(flagResume) && sessionFlags.resume == resumeSelectorFlag && len(arguments) > 0 {
				sessionFlags.resume = arguments[0]
				arguments = arguments[1:]
			}
			return runRootAgent(command, arguments, configFlags, projectFlags, sessionFlags, plain, runtime)
		},
	}

	configFlags.bind(command)
	projectFlags.bind(command)
	sessionFlags.bind(command)
	command.PersistentFlags().BoolVar(&plain, flagPlain, false, "use plain line-oriented terminal output")
	command.AddCommand(newConfigCommand(configFlags, runtime))
	command.AddCommand(newToolsCommand())
	command.AddCommand(newSessionsCommand(projectFlags, runtime))
	command.AddCommand(newWebCommand(configFlags, runtime))
	command.AddCommand(newVersionCommand(buildinfo.Current()))

	return command
}

func defaultCommandRuntime() commandRuntime {
	lookupEnv := config.EnvLookup(os.LookupEnv)
	amadeusRoot, err := resolveAmadeusRoot(lookupEnv, os.Executable, filepath.EvalSymlinks)
	workingDirectory, workingDirectoryErr := os.Getwd()
	return commandRuntime{
		amadeusRoot:         amadeusRoot,
		rootErr:             err,
		workingDirectory:    workingDirectory,
		workingDirectoryErr: workingDirectoryErr,
		lookupEnv:           lookupEnv,
		terminalDetector:    isTerminalInput,
		agentContextFactory: interruptibleTurnContext,
		agentCommandFactory: defaultAgentCommandFactory,
		auditSinkFactory:    defaultAuditSinkFactory(lookupEnv, os.UserHomeDir),
		threadStoreFactory:  defaultThreadStoreFactory,
		persistentIDFactory: nextPersistentID,
		now:                 time.Now,
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
