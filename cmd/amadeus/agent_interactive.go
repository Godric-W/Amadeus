package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/buildinfo"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/thread"
	"github.com/Godric-W/Amadeus/internal/tool/builtin"
	"github.com/atotto/clipboard"
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
	var lastAssistantMarkdown string
	controller, err := tui.NewTerminalInteractionController(interactionInput, invocation.Output, invocation.ErrorOutput,
		func(commandCtx context.Context, command string) error {
			spec, arguments, ok := tui.ParseSlashCommand(command)
			if !ok {
				return fmt.Errorf("unknown command %q", command)
			}
			switch spec.Command {
			case tui.SlashClear:
				if err := runner.newDraft(commandCtx); err != nil {
					return err
				}
				lastAssistantMarkdown = ""
				_, err := fmt.Fprint(invocation.ErrorOutput, "\x1b[2J\x1b[H")
				return err
			case tui.SlashResume:
				return runner.selectSession(commandCtx, invocation, reader)
			case tui.SlashStatus:
				return runner.writeInteractiveStatus(commandCtx, invocation)
			case tui.SlashSkills:
				return runner.writeInteractiveSkills(commandCtx, invocation, invocation.ErrorOutput)
			case tui.SlashMCP:
				return runner.writeInteractiveMCP(commandCtx, invocation, invocation.ErrorOutput, strings.EqualFold(arguments, "verbose"))
			case tui.SlashRename:
				if arguments == "" {
					return errors.New("/rename requires a session name in plain mode")
				}
				title, err := runner.renameCurrent(commandCtx, arguments)
				if err == nil {
					_, err = fmt.Fprintf(invocation.ErrorOutput, "Session renamed to %s\n", title)
				}
				return err
			case tui.SlashDelete:
				if _, err := fmt.Fprint(invocation.ErrorOutput, "Permanently delete this session and exit? [y/N] "); err != nil {
					return err
				}
				answer, readErr := reader.ReadString('\n')
				if readErr != nil && !errors.Is(readErr, io.EOF) {
					return readErr
				}
				if !strings.EqualFold(strings.TrimSpace(answer), "y") && !strings.EqualFold(strings.TrimSpace(answer), "yes") {
					_, err := fmt.Fprintln(invocation.ErrorOutput, "Session deletion cancelled")
					return err
				}
				deleted, err := runner.deleteCurrent(commandCtx)
				if err != nil {
					return err
				}
				if deleted == "" {
					fmt.Fprintln(invocation.ErrorOutput, "Draft session discarded")
				} else {
					fmt.Fprintf(invocation.ErrorOutput, "Session deleted: %s\n", deleted)
				}
				return tui.ErrQuit
			case tui.SlashCompact:
				message, err := runner.compactInteractiveSession(commandCtx, invocation)
				if err == nil {
					_, err = fmt.Fprintln(invocation.ErrorOutput, message)
				}
				return err
			case tui.SlashCopy:
				if strings.TrimSpace(lastAssistantMarkdown) == "" {
					return errors.New("No agent response to copy")
				}
				if err := clipboard.WriteAll(lastAssistantMarkdown); err != nil {
					return fmt.Errorf("copy last response: %w", err)
				}
				_, err := fmt.Fprintln(invocation.ErrorOutput, "Copied last response to clipboard")
				return err
			case tui.SlashExit:
				return tui.ErrQuit
			default:
				return fmt.Errorf("/%s is unavailable in plain mode", spec.Command)
			}
		},
		func(runCtx context.Context, submission tui.TaskSubmission) error {
			if len(submission.Content) > maxRootTaskBytes {
				return fmt.Errorf("interactive task exceeds %d bytes", maxRootTaskBytes)
			}
			runInvocation := invocation
			runInvocation.Mode = agentInvocationOnce
			runInvocation.Task = submission.Content
			runInvocation.RunMode = turn.PermissionModeDefault
			if submission.Mode == tui.CollaborationPlan {
				runInvocation.RunMode = turn.PermissionModePlan
			}
			runInvocation.Input = interactionInput
			var response bytes.Buffer
			runInvocation.Output = io.MultiWriter(invocation.Output, &response)
			err := runner.runOnce(runCtx, runInvocation)
			if errorAlreadyReported(err) {
				return nil
			}
			if err == nil {
				lastAssistantMarkdown = strings.TrimSpace(response.String())
			}
			return err
		},
	)
	if err != nil {
		return err
	}
	controller.WithTaskContextFactory(runner.newTurnContext)
	controller.WithCapabilities(capabilities)
	if err := controller.Run(ctx); err != nil {
		return err
	}
	_, err = fmt.Fprintln(invocation.ErrorOutput, "session: closed")
	return err
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
			runInvocation.RunMode = turn.PermissionModeDefault
			if submission.Mode == tui.CollaborationPlan {
				runInvocation.RunMode = turn.PermissionModePlan
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
		Command: func(commandCtx context.Context, command string) (string, error) {
			var output strings.Builder
			spec, arguments, ok := tui.ParseSlashCommand(command)
			if !ok {
				return "", fmt.Errorf("unknown command %q", command)
			}
			switch spec.Command {
			case tui.SlashStatus:
				statusInvocation := invocation
				statusInvocation.ErrorOutput = &output
				err := runner.writeInteractiveStatus(commandCtx, statusInvocation)
				return strings.TrimSpace(output.String()), err
			case tui.SlashMCP:
				err := runner.writeInteractiveMCP(commandCtx, invocation, &output, strings.EqualFold(arguments, "verbose"))
				return strings.TrimSpace(output.String()), err
			case tui.SlashClear:
				return "Started a new chat", runner.newDraft(commandCtx)
			default:
				return "", fmt.Errorf("command /%s is not delegated through the generic handler", spec.Command)
			}
		},
		Sessions: func(commandCtx context.Context) ([]tui.SessionOption, error) {
			threads, err := runner.listThreads(commandCtx, invocation)
			if err != nil {
				return nil, err
			}
			runner.threadMutex.Lock()
			current := runner.currentThread
			runner.threadMutex.Unlock()
			options := make([]tui.SessionOption, 0, len(threads))
			for _, metadata := range threads {
				options = append(options, tui.SessionOption{ID: string(metadata.ID), Title: metadata.Title, Current: current != nil && metadata.ID == current.ID()})
			}
			return options, nil
		},
		Resume: func(commandCtx context.Context, id string) (string, error) {
			manager, configured, err := runner.ensureThreadManager(commandCtx, invocation)
			if err != nil {
				return "", err
			}
			active, err := runner.resumeThread(commandCtx, manager, configured, invocation, thread.ID(id))
			if err != nil {
				return "", err
			}
			metadata, _ := runner.currentThreadMetadata(commandCtx)
			return fmt.Sprintf("Session resumed: %s (%s)", active.ID(), metadata.Title), nil
		},
		CurrentSession: func() string {
			runner.threadMutex.Lock()
			defer runner.threadMutex.Unlock()
			if runner.currentThread == nil {
				return "draft"
			}
			return string(runner.currentThread.ID())
		},
		CurrentSessionTitle: func() string {
			metadata, err := runner.currentThreadMetadata(context.Background())
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
			factory, err := runner.currentTaskFactory(commandCtx, invocation)
			if err != nil {
				return nil, err
			}
			extensions, err := factory.ensureExtensions()
			if err != nil || extensions.Skills() == nil {
				return nil, errors.New("Skill catalog is unavailable")
			}
			entries := extensions.Skills().Index()
			options := make([]tui.SkillOption, 0, len(entries))
			for _, entry := range entries {
				options = append(options, tui.SkillOption{Name: entry.Name, Description: entry.Description, Source: string(entry.Source), Enabled: entry.Enabled})
			}
			return options, commandCtx.Err()
		},
		SetSkill: func(commandCtx context.Context, name string, enabled bool) error {
			factory, err := runner.currentTaskFactory(commandCtx, invocation)
			if err != nil {
				return err
			}
			extensions, err := factory.ensureExtensions()
			if err != nil {
				return err
			}
			if err := extensions.SetSkillEnabled(name, enabled); err != nil {
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

func replayTurnItems(lines []rollout.Line) ([]protocol.TurnItem, error) {
	items, err := protocol.ProjectCompletedItems(lines)
	if err != nil {
		return nil, err
	}
	legacy, err := protocol.LegacyResponseItemsToCompleted(lines)
	if err != nil {
		return nil, err
	}
	if len(legacy) == 0 {
		return items, nil
	}
	// Old rollouts have response_item facts but no completed-item records.
	// Prefer the new records and only use legacy items when no new projection
	// exists for that stable item ID.
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		seen[item.ID] = struct{}{}
	}
	for _, item := range legacy {
		if _, exists := seen[item.ID]; exists {
			continue
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(left, right int) bool {
		return items[left].CompletedAt.Before(items[right].CompletedAt)
	})
	return items, nil
}

func (runner *agentController) compactInteractiveSession(ctx context.Context, invocation agentInvocation) (string, error) {
	active, _, err := runner.ensureActiveThread(ctx, invocation)
	if err != nil {
		return "", err
	}
	factory, err := runner.currentTaskFactory(ctx, invocation)
	if err != nil {
		return "", err
	}
	projection, err := agentcontext.ProjectRolloutMessages(active.History())
	if err != nil {
		return "", err
	}
	if len(projection.Messages) == 0 {
		return "", errors.New("There is no conversation to compact")
	}
	compactInvocation := invocation
	compactInvocation.Task = "compact context"
	result, err := factory.Prepare(ctx, compactInvocation)
	if err != nil {
		return "", err
	}
	if err := active.Submit(ctx, protocol.CompactOp{}); err != nil {
		return "", err
	}
	if err := runner.waitTurn(ctx, active, result, invocation.EventSink, invocation.Approvals); err != nil {
		return "", err
	}
	return "Conversation compacted", nil
}

func (runner *agentController) currentTaskFactory(ctx context.Context, invocation agentInvocation) (*codingTaskFactory, error) {
	active, _, err := runner.ensureActiveThread(ctx, invocation)
	if err != nil {
		return nil, err
	}
	runner.threadMutex.Lock()
	factory := runner.taskFactories[active.ID()]
	runner.threadMutex.Unlock()
	if factory == nil {
		return nil, errors.New("active thread task factory is unavailable")
	}
	return factory, nil
}

func (runner *agentController) writeInteractiveSkills(ctx context.Context, invocation agentInvocation, writer io.Writer) error {
	factory, err := runner.currentTaskFactory(ctx, invocation)
	if err != nil {
		return err
	}
	extensions, err := factory.ensureExtensions()
	if err != nil || extensions.Skills() == nil {
		return errors.New("Skill catalog is unavailable")
	}
	entries := extensions.Skills().Index()
	if len(entries) == 0 {
		_, err = fmt.Fprintln(writer, "No skills available.")
		return err
	}
	for _, entry := range entries {
		state := "enabled"
		if !entry.Enabled {
			state = "disabled"
		}
		if _, err := fmt.Fprintf(writer, "%s  %s  %s  %s\n", entry.Name, entry.Source, state, entry.Description); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (runner *agentController) writeInteractiveMCP(ctx context.Context, invocation agentInvocation, writer io.Writer, verbose bool) error {
	factory, err := runner.currentTaskFactory(ctx, invocation)
	if err != nil {
		return err
	}
	extensions, err := factory.ensureExtensions()
	if err != nil || extensions.MCP() == nil {
		return errors.New("MCP manager is unavailable")
	}
	servers := extensions.MCP().EnabledServers()
	sort.Strings(servers)
	if len(servers) == 0 {
		_, err = fmt.Fprintln(writer, "No MCP servers configured.")
		return err
	}
	bindings := extensions.MCP().BindingSnapshot()
	byName := make(map[string]bool, len(bindings.Servers))
	for _, binding := range bindings.Servers {
		byName[binding.Name] = binding.ToolsLoaded
	}
	for _, server := range servers {
		if !verbose {
			state := "not started"
			if byName[server] {
				state = "tools loaded"
			}
			if _, err := fmt.Fprintf(writer, "%s  %s\n", server, state); err != nil {
				return err
			}
			continue
		}
		tools, listErr := extensions.MCP().ListTools(ctx, server)
		if listErr != nil {
			if _, err := fmt.Fprintf(writer, "%s  error: %v\n", server, listErr); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(writer, "%s  %d tools\n", server, len(tools)); err != nil {
			return err
		}
		for _, remote := range tools {
			if _, err := fmt.Fprintf(writer, "  %s  %s\n", remote.Name, remote.Description); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeInteractiveTools(writer io.Writer) error {
	for _, spec := range builtin.CoreSpecs() {
		if _, err := fmt.Fprintf(writer, "%s (%s)\n", spec.Name, spec.SideEffect); err != nil {
			return err
		}
	}
	return nil
}
