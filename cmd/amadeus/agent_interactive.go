package main

import (
	"context"
	"errors"
	"io"

	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/buildinfo"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
)

func (runner *agentController) runInteractive(ctx context.Context, invocation agentInvocation) error {
	if invocation.Input == nil || invocation.Output == nil || invocation.ErrorOutput == nil {
		return errors.New("interactive Coding Agent streams are nil")
	}
	detectTerminal := runner.runtime.terminalDetector
	if detectTerminal == nil {
		detectTerminal = isTerminalInput
	}
	capabilities := tui.DetectTerminalCapabilitiesWithOptions(invocation.Input, invocation.Output, tui.TerminalCapabilityOptions{
		IsTerminal: func(input io.Reader) bool { return detectTerminal(input) },
	})
	if !capabilities.TTY {
		return errors.New("interactive Amadeus requires a terminal")
	}
	if invocation.SessionMode != sessionStartSelect {
		if err := runner.prepareSession(ctx, invocation, nil); err != nil {
			return err
		}
	}
	return runner.runFullscreenInteractive(ctx, invocation, capabilities)
}

func (runner *agentController) runFullscreenInteractive(ctx context.Context, invocation agentInvocation, capabilities tui.TerminalCapabilities) error {
	workspace, configured, err := runner.ensureWorkspace(ctx, invocation)
	if err != nil {
		return err
	}
	provider := configured.Providers[configured.DefaultProvider]
	interactive, err := application.NewInteractiveApplication(ctx, application.InteractiveOptions{
		Workspace: workspace, Configuration: sessionConfiguration(configured, invocation),
		Project: invocation.Project.Path(), Provider: configured.DefaultProvider, Model: provider.Model,
		ContextWindow: provider.ContextWindow, MaxTaskBytes: maxRootTaskBytes,
	})
	if err != nil {
		return err
	}
	defer interactive.Close()
	snapshot, err := interactive.Start(ctx)
	if err != nil {
		return err
	}
	fullscreen, err := tui.NewFullscreenApplication(tui.FullscreenOptions{
		Input: invocation.Input, Output: invocation.Output,
		OpenSessions: invocation.SessionMode == sessionStartSelect,
		NoColor:      !capabilities.Color, Width: capabilities.Width,
		Snapshot: snapshot, Application: interactive,
		Startup: tui.FullscreenStartup{
			Version: buildinfo.Current().Version, Provider: snapshot.Provider, Model: snapshot.Model,
			Project: invocation.Project.Path(), Branch: tui.ResolveWorkspaceBranch(ctx, invocation.Project.Path()),
			Session: string(snapshot.ThreadID), ContextWindow: snapshot.ContextWindow,
		},
	})
	if err != nil {
		return err
	}
	return fullscreen.Run(ctx)
}
