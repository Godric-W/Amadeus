package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/buildinfo"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
	"github.com/Godric-W/Amadeus/internal/thread"
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
	active, configured, err := runner.ensureActiveThread(ctx, invocation)
	if err != nil {
		return err
	}
	initialItems, err := replayTurnItems(active.History())
	if err != nil {
		return err
	}
	provider := configured.Providers[configured.DefaultProvider]
	branch := tui.ResolveWorkspaceBranch(ctx, invocation.Project.Path())
	var application *tui.FullscreenApplication
	application, err = tui.NewFullscreenApplication(tui.FullscreenOptions{
		Input: invocation.Input, Output: invocation.Output, OpenSessions: invocation.SessionMode == sessionStartSelect,
		InitialItems: initialItems,
		NoColor:      !capabilities.Color, Width: capabilities.Width,
		Startup: tui.FullscreenStartup{
			Version: buildinfo.Current().Version, Provider: configured.DefaultProvider, Model: provider.Model,
			Project: invocation.Project.Path(), Branch: branch, Session: string(active.ID()), ContextWindow: provider.ContextWindow,
		},
		NewTask: runner.newTurnContext,
		Task: func(runCtx context.Context, submission tui.TaskSubmission) error {
			if len(submission.Content) > maxRootTaskBytes {
				return fmt.Errorf("interactive task exceeds %d bytes", maxRootTaskBytes)
			}
			runInvocation := invocation
			runInvocation.Mode = agentInvocationOnce
			runInvocation.Task = submission.Content
			runInvocation.RunMode = turn.ModeKindDefault
			if submission.Mode == tui.CollaborationPlan {
				runInvocation.RunMode = turn.ModeKindPlan
			}
			runInvocation.Input = strings.NewReader("")
			runInvocation.Output = io.Discard
			runInvocation.ErrorOutput = io.Discard
			runInvocation.EventSink = application
			runInvocation.Approvals = application
			err := runner.runOnce(runCtx, runInvocation)
			if errorAlreadyReported(err) {
				return nil
			}
			return err
		},
		Status: func(commandCtx context.Context) (string, error) {
			var output strings.Builder
			statusInvocation := invocation
			statusInvocation.ErrorOutput = &output
			err := runner.writeInteractiveStatus(commandCtx, statusInvocation)
			return strings.TrimSpace(output.String()), err
		},
		MCP: func(commandCtx context.Context, verbose bool) (string, error) {
			var output strings.Builder
			err := runner.writeInteractiveMCP(commandCtx, invocation, &output, verbose)
			return strings.TrimSpace(output.String()), err
		},
		SetPermissionMode: func(commandCtx context.Context, mode tui.CollaborationMode) error {
			activeThread, _, err := runner.ensureActiveThread(commandCtx, invocation)
			if err != nil {
				return err
			}
			permissionMode := turn.ModeKindDefault
			if mode == tui.CollaborationPlan {
				permissionMode = turn.ModeKindPlan
			}
			return activeThread.Submit(commandCtx, protocol.ThreadSettingsOp{Mode: string(permissionMode)})
		},
		Clear: func(commandCtx context.Context) error {
			return runner.newDraft(commandCtx)
		},
		Sessions: func(commandCtx context.Context) ([]tui.SessionOption, error) {
			threads, err := runner.listThreads(commandCtx, invocation)
			if err != nil {
				return nil, err
			}
			workspace := runner.currentWorkspace()
			current, _ := workspace.Current()
			options := make([]tui.SessionOption, 0, len(threads))
			for _, metadata := range threads {
				options = append(options, tui.SessionOption{ID: string(metadata.ID), Title: metadata.Title, Current: current != nil && metadata.ID == current.ID()})
			}
			return options, nil
		},
		Resume: func(commandCtx context.Context, id string) (string, error) {
			workspace, configured, err := runner.ensureWorkspace(commandCtx, invocation)
			if err != nil {
				return "", err
			}
			active, err := workspace.Resume(commandCtx, thread.ID(id), sessionConfiguration(configured, invocation))
			if err != nil {
				return "", err
			}
			metadata, _ := workspace.CurrentMetadata(commandCtx)
			return fmt.Sprintf("Session resumed: %s (%s)", active.ID(), metadata.Title), nil
		},
		CurrentSession: func() string {
			workspace := runner.currentWorkspace()
			if workspace == nil {
				return "draft"
			}
			current, ok := workspace.Current()
			if !ok {
				return "draft"
			}
			return string(current.ID())
		},
		CurrentSessionTitle: func() string {
			workspace := runner.currentWorkspace()
			if workspace == nil {
				return "draft"
			}
			metadata, err := workspace.CurrentMetadata(context.Background())
			if err != nil {
				return "draft"
			}
			return metadata.Title
		},
		Rename: func(commandCtx context.Context, title string) (string, error) {
			normalized, err := runner.renameCurrent(commandCtx, title)
			return "Session renamed to " + normalized, err
		},
		Delete: func(commandCtx context.Context) (string, error) {
			deleted, err := runner.deleteCurrent(commandCtx)
			if err != nil {
				return "", err
			}
			if deleted == "" {
				return "Draft session discarded", nil
			}
			return "Session deleted: " + string(deleted), nil
		},
		Compact: func(commandCtx context.Context) (string, error) {
			return runner.compactInteractiveSession(commandCtx, invocation)
		},
		Skills: func(commandCtx context.Context) ([]tui.SkillOption, error) {
			capabilities, err := runner.currentCapabilities(commandCtx, invocation)
			if err != nil {
				return nil, err
			}
			entries := capabilities.Skills()
			options := make([]tui.SkillOption, 0, len(entries))
			for _, entry := range entries {
				options = append(options, tui.SkillOption{Name: entry.Name, Description: entry.Description, Source: string(entry.Source), Enabled: entry.Enabled})
			}
			return options, commandCtx.Err()
		},
		SetSkill: func(commandCtx context.Context, name string, enabled bool) error {
			capabilities, err := runner.currentCapabilities(commandCtx, invocation)
			if err != nil {
				return err
			}
			if err := capabilities.SetSkillEnabled(name, enabled); err != nil {
				return err
			}
			return commandCtx.Err()
		},
	})
	if err != nil {
		return err
	}
	return application.Run(ctx)
}
