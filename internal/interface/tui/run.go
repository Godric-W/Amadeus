package tui

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/bootstrap"
	"github.com/Godric-W/Amadeus/internal/buildinfo"
)

type RunOptions struct {
	Bootstrap           bootstrap.WorkspaceOptions
	Target              app.ThreadTarget
	OpenSessions        bool
	Prompt              string
	MaxUserMessageBytes int

	Input       io.Reader
	Output      io.Writer
	ErrorOutput io.Writer
	IsTerminal  func(io.Reader) bool
}

type RunResult struct {
	ExitInfo AppExitInfo
	Color    bool
}

func Run(ctx context.Context, options RunOptions) (result RunResult, runErr error) {
	if ctx == nil {
		return RunResult{}, errors.New("interactive context is nil")
	}
	if options.Input == nil || options.Output == nil || options.ErrorOutput == nil {
		return RunResult{}, errors.New("interactive streams are nil")
	}
	if err := options.Target.Validate(); err != nil {
		return RunResult{}, err
	}
	capabilities := DetectTerminalCapabilitiesWithOptions(options.Input, options.Output, TerminalCapabilityOptions{
		IsTerminal: options.IsTerminal,
	})
	if !capabilities.TTY {
		return RunResult{}, errors.New("interactive Amadeus requires a terminal")
	}
	result.Color = capabilities.Color

	bootstrapped, err := bootstrap.OpenWorkspace(ctx, options.Bootstrap)
	if err != nil {
		return result, err
	}
	defer func() {
		runErr = errors.Join(runErr, bootstrap.CloseWorkspace(bootstrapped.Workspace))
	}()
	start, err := bootstrapped.Workspace.PrepareStart(ctx, options.Target, bootstrapped.Configuration)
	if err != nil {
		return result, err
	}
	if err := presentInteractiveThreadStart(options.ErrorOutput, options.Target, start); err != nil {
		return result, err
	}

	interactive, err := app.NewInteractiveApplication(ctx, app.InteractiveOptions{
		Workspace: bootstrapped.Workspace, Configuration: bootstrapped.Configuration,
		MaxUserMessageBytes: options.MaxUserMessageBytes,
	})
	if err != nil {
		return result, err
	}
	defer interactive.Close()
	snapshot, err := interactive.Start(ctx)
	if err != nil {
		return result, err
	}
	fullscreen, err := NewFullscreenApplication(FullscreenOptions{
		Input: options.Input, Output: options.Output,
		OpenSessions:       options.OpenSessions,
		InitialUserMessage: createInitialUserMessage(options.Prompt),
		NoColor:            !capabilities.Color, Width: capabilities.Width,
		Snapshot: snapshot, Application: interactive,
		Startup: FullscreenStartup{Version: buildinfo.Current().Version},
	})
	if err != nil {
		return result, err
	}
	result.ExitInfo, runErr = fullscreen.Run(ctx)
	return result, runErr
}

func presentInteractiveThreadStart(output io.Writer, target app.ThreadTarget, start app.ThreadStartResult) error {
	switch target.Kind {
	case app.ThreadTargetLatest:
		if start.Draft {
			_, err := fmt.Fprintln(output, "session: no previous session; using a new draft")
			return err
		}
		if start.Active != nil && start.Metadata != nil {
			_, err := fmt.Fprintf(output, "session: continued %s (%s)\n", start.Active.ID(), start.Metadata.Title)
			return err
		}
	case app.ThreadTargetResume:
		title := ""
		if start.Metadata != nil {
			title = start.Metadata.Title
		}
		_, err := fmt.Fprintf(output, "session: resumed %s (%s)\n", target.ThreadID, title)
		return err
	}
	return nil
}
