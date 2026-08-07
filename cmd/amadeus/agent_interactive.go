package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Godric-W/Amadeus/internal/buildinfo"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
	"github.com/Godric-W/Amadeus/internal/tool/builtin"
)

func (runner *agentController) runInteractive(ctx context.Context, invocation agentInvocation) error {
	if invocation.Input == nil || invocation.Output == nil || invocation.ErrorOutput == nil {
		return errors.New("interactive Coding Agent streams are nil")
	}
	reader := bufio.NewReader(invocation.Input)
	detectTerminal := runner.runtime.terminalDetector
	if detectTerminal == nil {
		detectTerminal = isTerminalInput
	}
	capabilities := tui.DetectTerminalCapabilitiesWithOptions(invocation.Input, invocation.Output, tui.TerminalCapabilityOptions{
		IsTerminal: func(input io.Reader) bool { return detectTerminal(input) }, ForcePlain: invocation.Plain,
	})
	fullscreen := capabilities.TTY && !capabilities.Plain
	if !(fullscreen && invocation.SessionMode == sessionStartSelect) {
		if err := runner.prepareSession(ctx, invocation, reader); err != nil {
			return err
		}
	}
	if fullscreen {
		return runner.runFullscreenInteractive(ctx, invocation, capabilities)
	}
	return runner.runPlainInteractive(ctx, invocation, reader, capabilities)
}

func (runner *agentController) runPlainInteractive(ctx context.Context, invocation agentInvocation, reader *bufio.Reader, capabilities tui.TerminalCapabilities) error {
	interactionInput := io.Reader(reader)
	interactionReader := reader
	controller, err := tui.NewTerminalInteractionController(interactionInput, invocation.Output, invocation.ErrorOutput,
		func(commandCtx context.Context, command string) error {
			switch command {
			case "/help":
				_, err := fmt.Fprintf(invocation.ErrorOutput, "commands: %s\n", strings.Join(tui.SlashCommands(), ", "))
				return err
			case "/clear":
				_, err := fmt.Fprint(invocation.ErrorOutput, "\x1b[2J\x1b[H")
				return err
			case "/resume":
				selector := interactionReader
				if selector == nil {
					selector = bufio.NewReader(invocation.Input)
				}
				return runner.selectSession(commandCtx, invocation, selector)
			case "/status":
				return runner.writeInteractiveStatus(commandCtx, invocation)
			case "/tools":
				return writeInteractiveTools(invocation.ErrorOutput)
			default:
				return fmt.Errorf("unknown command %q", command)
			}
		},
		func(runCtx context.Context, task string) error {
			if len(task) > maxRootTaskBytes {
				return fmt.Errorf("interactive task exceeds %d bytes", maxRootTaskBytes)
			}
			runInvocation := invocation
			runInvocation.Mode = agentInvocationOnce
			runInvocation.Task = task
			runInvocation.Input = interactionInput
			err := runner.runOnce(runCtx, runInvocation)
			if errorAlreadyReported(err) {
				return nil
			}
			return err
		},
	)
	if err != nil {
		return err
	}
	controller.WithTaskContextFactory(runner.newRunContext)
	controller.WithCapabilities(capabilities)
	err = controller.Run(ctx)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(invocation.ErrorOutput, "session: closed")
	return err
}

func (runner *agentController) runFullscreenInteractive(ctx context.Context, invocation agentInvocation, capabilities tui.TerminalCapabilities) error {
	configured, _, err := loadEffectiveConfig(runner.command, runner.flags, runner.runtime)
	if err != nil {
		return err
	}
	if err := config.Validate(configured); err != nil {
		return err
	}
	provider := configured.Providers[configured.DefaultProvider]
	sessionRuntime, err := runner.ensureSessionRuntime(ctx, invocation)
	if err != nil {
		return err
	}
	sessionID := string(sessionRuntime.CurrentSessionID())
	if sessionID == "" {
		sessionID = "draft"
	}
	branch := tui.ResolveWorkspaceBranch(ctx, invocation.Project.Path())
	var application *tui.FullscreenApplication
	application, err = tui.NewFullscreenApplication(tui.FullscreenOptions{
		Input: invocation.Input, Output: invocation.Output, OpenSessions: invocation.SessionMode == sessionStartSelect,
		NoColor: !capabilities.Color, Width: capabilities.Width,
		Startup: tui.FullscreenStartup{
			Version: buildinfo.Current().Version, Provider: configured.DefaultProvider, Model: provider.Model,
			Project: invocation.Project.Path(), Branch: branch, Session: sessionID, ContextWindow: provider.ContextWindow,
		},
		NewTask: runner.newRunContext,
		Task: func(runCtx context.Context, task string) error {
			if len(task) > maxRootTaskBytes {
				return fmt.Errorf("interactive task exceeds %d bytes", maxRootTaskBytes)
			}
			runInvocation := invocation
			runInvocation.Mode = agentInvocationOnce
			runInvocation.Task = task
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
		Command: func(commandCtx context.Context, command string) (string, error) {
			var output strings.Builder
			switch strings.Fields(command)[0] {
			case "/status":
				statusInvocation := invocation
				statusInvocation.ErrorOutput = &output
				err := runner.writeInteractiveStatus(commandCtx, statusInvocation)
				return strings.TrimSpace(output.String()), err
			case "/tools":
				err := writeInteractiveTools(&output)
				return strings.TrimSpace(output.String()), err
			default:
				return "", fmt.Errorf("unknown command %q", command)
			}
		},
		Sessions: func(commandCtx context.Context) ([]tui.SessionOption, error) {
			sessions, listErr := sessionRuntime.ListSessions(commandCtx)
			if listErr != nil {
				return nil, listErr
			}
			options := make([]tui.SessionOption, 0, len(sessions))
			for _, conversation := range sessions {
				options = append(options, tui.SessionOption{ID: string(conversation.ID), Title: conversation.Title, Current: conversation.ID == sessionRuntime.CurrentSessionID()})
			}
			return options, nil
		},
		Resume: func(commandCtx context.Context, id string) (string, error) {
			conversation, resumeErr := sessionRuntime.Resume(commandCtx, sessiondomain.SessionID(id))
			if resumeErr != nil {
				return "", resumeErr
			}
			return fmt.Sprintf("Session resumed: %s (%s)", conversation.ID, conversation.Title), nil
		},
		CurrentSession: func() string { return string(sessionRuntime.CurrentSessionID()) },
	})
	if err != nil {
		return err
	}
	return application.Run(ctx)
}

func writeInteractiveTools(writer io.Writer) error {
	for _, spec := range builtin.MVPSpecs() {
		if _, err := fmt.Fprintf(writer, "%s (%s)\n", spec.Name, spec.SideEffect); err != nil {
			return err
		}
	}
	return nil
}
