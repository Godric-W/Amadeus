package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/bootstrap"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/tui"
	"github.com/spf13/cobra"
)

const maxInitialPromptBytes = 1 << 20

type tuiLaunch struct {
	Prompt       string
	Project      project.Root
	ExtraRoots   []string
	Target       app.ThreadTarget
	OpenSessions bool
	Input        io.Reader
	Output       io.Writer
	ErrorOutput  io.Writer
}

func runRootAgent(command *cobra.Command, arguments []string, configFlags *configFlags, projectFlags *projectFlags, sessionFlags *sessionFlags, options RootOptions) error {
	launch, err := resolveTUILaunch(command, arguments)
	if err != nil {
		return err
	}
	launch.Project, err = projectFlags.resolve(command, options.Environment)
	if err != nil {
		return err
	}
	launch.ExtraRoots, err = projectFlags.resolveAdditional(options.Environment, launch.Project)
	if err != nil {
		return err
	}
	startMode, resumeID, err := sessionFlags.resolve(command)
	if err != nil {
		return err
	}
	launch.Target, launch.OpenSessions, err = threadTarget(startMode, resumeID)
	if err != nil {
		return err
	}
	configured, _, err := loadEffectiveConfig(command, configFlags, options.Environment)
	if err != nil {
		return err
	}
	workspaceOptions := bootstrap.WorkspaceOptions{
		AmadeusRoot:   options.Environment.AmadeusRoot,
		Configuration: configured,
		CWD:           launch.Project.Path(), WorkspaceRoots: launch.ExtraRoots,
		Mode: protocol.ModeKindDefault, Dependencies: options.Bootstrap,
	}

	result, runErr := options.TUI(command.Context(), tui.RunOptions{
		Bootstrap: workspaceOptions, Target: launch.Target, OpenSessions: launch.OpenSessions,
		Prompt: launch.Prompt, MaxUserMessageBytes: maxInitialPromptBytes,
		Input: launch.Input, Output: launch.Output, ErrorOutput: launch.ErrorOutput,
		IsTerminal: options.IsTerminal,
	})
	exitErr := presentTUIExit(result.ExitInfo, launch.Output, launch.ErrorOutput, result.Color)
	return errors.Join(runErr, exitErr)
}

func resolveTUILaunch(command *cobra.Command, arguments []string) (tuiLaunch, error) {
	if command == nil {
		return tuiLaunch{}, errors.New("root Agent command is nil")
	}
	if len(arguments) > 1 {
		return tuiLaunch{}, fmt.Errorf("accepts at most one prompt argument, received %d", len(arguments))
	}
	launch := tuiLaunch{Input: command.InOrStdin(), Output: command.OutOrStdout(), ErrorOutput: command.ErrOrStderr()}
	if len(arguments) == 1 {
		prompt := normalizePromptLineEndings(arguments[0])
		if len(prompt) > maxInitialPromptBytes {
			return tuiLaunch{}, fmt.Errorf("prompt argument exceeds %d byte limit", maxInitialPromptBytes)
		}
		launch.Prompt = prompt
	}
	return launch, nil
}

func threadTarget(mode sessionStartMode, resumeID protocol.ThreadID) (app.ThreadTarget, bool, error) {
	switch mode {
	case "", sessionStartDraft:
		return app.ThreadTarget{Kind: app.ThreadTargetNew}, false, nil
	case sessionStartContinue:
		return app.ThreadTarget{Kind: app.ThreadTargetLatest}, false, nil
	case sessionStartResume:
		return app.ThreadTarget{Kind: app.ThreadTargetResume, ThreadID: resumeID}, false, nil
	case sessionStartSelect:
		return app.ThreadTarget{Kind: app.ThreadTargetNew}, true, nil
	default:
		return app.ThreadTarget{}, false, fmt.Errorf("unsupported session start mode %q", mode)
	}
}

func normalizePromptLineEndings(prompt string) string {
	prompt = strings.ReplaceAll(prompt, "\r\n", "\n")
	return strings.ReplaceAll(prompt, "\r", "\n")
}
