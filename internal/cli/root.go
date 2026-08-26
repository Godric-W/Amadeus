package cli

import (
	"context"
	"io"

	"github.com/Godric-W/Amadeus/internal/bootstrap"
	"github.com/Godric-W/Amadeus/internal/buildinfo"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
	"github.com/spf13/cobra"
)

type TUIRunner func(context.Context, tui.RunOptions) (tui.RunResult, error)

type RootOptions struct {
	Environment bootstrap.Environment
	Bootstrap   bootstrap.Dependencies
	TUI         TUIRunner
	IsTerminal  func(io.Reader) bool
}

func DefaultRootOptions() RootOptions {
	environment := bootstrap.CurrentEnvironment()
	return RootOptions{
		Environment: environment,
		Bootstrap:   bootstrap.DefaultDependencies(environment),
		TUI:         tui.Run,
	}
}

func NewRootCommand(options RootOptions) *cobra.Command {
	return newRootCommandWithFlags(&configFlags{}, &projectFlags{}, normalizeRootOptions(options))
}

func newRootCommand() *cobra.Command {
	return NewRootCommand(DefaultRootOptions())
}

func newRootCommandWithConfigFlags(flags *configFlags) *cobra.Command {
	return newRootCommandWithFlags(flags, &projectFlags{}, DefaultRootOptions())
}

func newRootCommandWithOptions(flags *configFlags, options RootOptions) *cobra.Command {
	return newRootCommandWithFlags(flags, &projectFlags{}, normalizeRootOptions(options))
}

func newRootCommandWithFlags(configFlags *configFlags, projectFlags *projectFlags, options RootOptions) *cobra.Command {
	options = normalizeRootOptions(options)
	sessionFlags := &sessionFlags{}
	command := &cobra.Command{
		Use:           "amadeus [PROMPT]",
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
			return runRootAgent(command, arguments, configFlags, projectFlags, sessionFlags, options)
		},
	}

	configFlags.bind(command)
	projectFlags.bind(command)
	sessionFlags.bind(command)
	command.AddCommand(newConfigCommand(configFlags, options.Environment))
	command.AddCommand(newToolsCommand())
	command.AddCommand(newSessionsCommand(projectFlags, options))
	command.AddCommand(newWebCommand(configFlags, options))
	command.AddCommand(newVersionCommand(buildinfo.Current()))
	return command
}

func normalizeRootOptions(options RootOptions) RootOptions {
	defaults := bootstrap.DefaultDependencies(options.Environment)
	if options.Bootstrap.AuditFactory == nil {
		options.Bootstrap.AuditFactory = defaults.AuditFactory
	}
	if options.Bootstrap.ThreadStore == nil {
		options.Bootstrap.ThreadStore = defaults.ThreadStore
	}
	if options.Bootstrap.Clock == nil {
		options.Bootstrap.Clock = defaults.Clock
	}
	if options.Bootstrap.NextID == nil {
		options.Bootstrap.NextID = defaults.NextID
	}
	if options.TUI == nil {
		options.TUI = tui.Run
	}
	return options
}
