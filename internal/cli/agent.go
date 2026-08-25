package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/bootstrap"
	agentexec "github.com/Godric-W/Amadeus/internal/exec"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/spf13/cobra"
)

const maxRootTaskBytes = 1 << 20

type agentLaunchMode string

const (
	agentLaunchOnce        agentLaunchMode = "once"
	agentLaunchInteractive agentLaunchMode = "interactive"
)

type agentLaunch struct {
	Mode         agentLaunchMode
	Task         string
	Project      project.Root
	ExtraRoots   []string
	Target       app.ThreadTarget
	OpenSessions bool
	Input        io.Reader
	Output       io.Writer
	ErrorOutput  io.Writer
}

func runRootAgent(command *cobra.Command, arguments []string, configFlags *configFlags, projectFlags *projectFlags, sessionFlags *sessionFlags, options RootOptions) error {
	launch, err := resolveAgentLaunch(command, arguments, options.IsTerminal)
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
	launch.Target, launch.OpenSessions, err = threadTarget(startMode, resumeID, launch.Mode)
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
		Mode: turn.ModeKindDefault, Dependencies: options.Bootstrap,
	}

	if launch.Mode == agentLaunchInteractive {
		result, runErr := options.Runners.TUI(command.Context(), tui.RunOptions{
			Bootstrap: workspaceOptions, Target: launch.Target, OpenSessions: launch.OpenSessions,
			MaxTaskBytes: maxRootTaskBytes,
			Input:        launch.Input, Output: launch.Output, ErrorOutput: launch.ErrorOutput,
			IsTerminal: options.IsTerminal,
		})
		exitErr := presentFullscreenExit(result.ExitInfo, launch.Output, launch.ErrorOutput, result.Color)
		return errors.Join(runErr, exitErr)
	}
	return options.Runners.Exec(command.Context(), agentexec.Options{
		Bootstrap: workspaceOptions, Target: launch.Target, Task: launch.Task,
		Input: launch.Input, Output: launch.Output, ErrorOutput: launch.ErrorOutput,
		IsTerminal: options.IsTerminal,
	})
}

func resolveAgentLaunch(command *cobra.Command, arguments []string, detectTerminal func(io.Reader) bool) (agentLaunch, error) {
	if command == nil {
		return agentLaunch{}, errors.New("root Agent command is nil")
	}
	if len(arguments) > 1 {
		return agentLaunch{}, fmt.Errorf("accepts at most one task argument, received %d", len(arguments))
	}
	launch := agentLaunch{Input: command.InOrStdin(), Output: command.OutOrStdout(), ErrorOutput: command.ErrOrStderr()}
	if len(arguments) == 1 {
		task := strings.TrimSpace(arguments[0])
		if task == "" {
			return agentLaunch{}, errors.New("task argument is empty")
		}
		launch.Mode, launch.Task = agentLaunchOnce, task
		return launch, nil
	}
	if detectTerminal == nil {
		detectTerminal = isTerminalInput
	}
	if detectTerminal(launch.Input) {
		launch.Mode = agentLaunchInteractive
		return launch, nil
	}
	task, err := readRootTask(launch.Input)
	if err != nil {
		return agentLaunch{}, err
	}
	launch.Mode, launch.Task = agentLaunchOnce, task
	return launch, nil
}

func threadTarget(mode sessionStartMode, resumeID protocol.ThreadID, launchMode agentLaunchMode) (app.ThreadTarget, bool, error) {
	switch mode {
	case "", sessionStartDraft:
		return app.ThreadTarget{Kind: app.ThreadTargetNew}, false, nil
	case sessionStartContinue:
		return app.ThreadTarget{Kind: app.ThreadTargetLatest}, false, nil
	case sessionStartResume:
		return app.ThreadTarget{Kind: app.ThreadTargetResume, ThreadID: resumeID}, false, nil
	case sessionStartSelect:
		if launchMode != agentLaunchInteractive {
			return app.ThreadTarget{}, false, errors.New("--resume without a session ID requires interactive terminal mode")
		}
		return app.ThreadTarget{Kind: app.ThreadTargetNew}, true, nil
	default:
		return app.ThreadTarget{}, false, fmt.Errorf("unsupported session start mode %q", mode)
	}
}

func readRootTask(input io.Reader) (string, error) {
	if input == nil {
		return "", errors.New("root Agent stdin is nil")
	}
	content, err := io.ReadAll(io.LimitReader(input, maxRootTaskBytes+1))
	if err != nil {
		return "", fmt.Errorf("read task from stdin: %w", err)
	}
	if len(content) > maxRootTaskBytes {
		return "", fmt.Errorf("task from stdin exceeds %d byte limit", maxRootTaskBytes)
	}
	task := strings.TrimSpace(string(content))
	if task == "" {
		return "", errors.New("stdin is not a TTY and did not contain a task")
	}
	return task, nil
}

func isTerminalInput(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
