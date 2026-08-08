package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Godric-W/Amadeus/internal/buildinfo"
	"github.com/Godric-W/Amadeus/internal/config"
	extensionruntime "github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
	"github.com/Godric-W/Amadeus/internal/llm"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
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
	interactionReader := reader
	var lastAssistantMarkdown string
	ensureRuntime := func(commandCtx context.Context) (*sessiondomain.SessionRuntime, error) {
		return runner.ensureSessionRuntime(commandCtx, invocation)
	}
	controller, err := tui.NewTerminalInteractionController(interactionInput, invocation.Output, invocation.ErrorOutput,
		func(commandCtx context.Context, command string) error {
			spec, arguments, ok := tui.ParseSlashCommand(command)
			if !ok {
				return fmt.Errorf("unknown command %q", command)
			}
			switch spec.Command {
			case tui.SlashClear:
				sessionRuntime, runtimeErr := ensureRuntime(commandCtx)
				if runtimeErr != nil {
					return runtimeErr
				}
				if err := sessionRuntime.NewDraft(); err != nil {
					return err
				}
				lastAssistantMarkdown = ""
				_, err := fmt.Fprint(invocation.ErrorOutput, "\x1b[2J\x1b[H")
				return err
			case tui.SlashResume:
				selector := interactionReader
				if selector == nil {
					selector = bufio.NewReader(invocation.Input)
				}
				return runner.selectSession(commandCtx, invocation, selector)
			case tui.SlashStatus:
				return runner.writeInteractiveStatus(commandCtx, invocation)
			case tui.SlashSkills:
				sessionRuntime, runtimeErr := ensureRuntime(commandCtx)
				if runtimeErr != nil {
					return runtimeErr
				}
				return writeInteractiveSkills(commandCtx, invocation.ErrorOutput, sessionRuntime)
			case tui.SlashMCP:
				sessionRuntime, runtimeErr := ensureRuntime(commandCtx)
				if runtimeErr != nil {
					return runtimeErr
				}
				return writeInteractiveMCP(commandCtx, invocation.ErrorOutput, sessionRuntime, strings.EqualFold(arguments, "verbose"))
			case tui.SlashRename:
				if arguments == "" {
					return errors.New("/rename requires a session name in plain mode")
				}
				sessionRuntime, runtimeErr := ensureRuntime(commandCtx)
				if runtimeErr != nil {
					return runtimeErr
				}
				_, title, err := sessionRuntime.RenameCurrent(commandCtx, arguments)
				if err == nil {
					_, err = fmt.Fprintf(invocation.ErrorOutput, "Session renamed to %s\n", title)
				}
				return err
			case tui.SlashDelete:
				sessionRuntime, runtimeErr := ensureRuntime(commandCtx)
				if runtimeErr != nil {
					return runtimeErr
				}
				if interactionReader == nil {
					return errors.New("session deletion confirmation requires interactive input")
				}
				if _, err := fmt.Fprint(invocation.ErrorOutput, "Permanently delete this session and exit? [y/N] "); err != nil {
					return err
				}
				answer, readErr := interactionReader.ReadString('\n')
				if readErr != nil && !errors.Is(readErr, io.EOF) {
					return readErr
				}
				if !strings.EqualFold(strings.TrimSpace(answer), "y") && !strings.EqualFold(strings.TrimSpace(answer), "yes") {
					_, writeErr := fmt.Fprintln(invocation.ErrorOutput, "Session deletion cancelled")
					return writeErr
				}
				deleted, deleteErr := sessionRuntime.DeleteCurrent(commandCtx)
				if deleteErr != nil {
					return deleteErr
				}
				if deleted == "" {
					_, deleteErr = fmt.Fprintln(invocation.ErrorOutput, "Draft session discarded")
				} else {
					_, deleteErr = fmt.Fprintf(invocation.ErrorOutput, "Session deleted: %s\n", deleted)
				}
				if deleteErr != nil {
					return deleteErr
				}
				return tui.ErrQuit
			case tui.SlashCompact:
				sessionRuntime, runtimeErr := ensureRuntime(commandCtx)
				if runtimeErr != nil {
					return runtimeErr
				}
				configured, _, configErr := loadEffectiveConfig(runner.command, runner.flags, runner.runtime)
				if configErr != nil {
					return configErr
				}
				message, compactErr := runner.compactInteractiveSession(commandCtx, configured, sessionRuntime)
				if compactErr == nil {
					_, compactErr = fmt.Fprintln(invocation.ErrorOutput, message)
				}
				return compactErr
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
			if submission.Mode == tui.CollaborationPlan {
				runInvocation.RunMode = sessiondomain.RunModePlan
			} else {
				runInvocation.RunMode = sessiondomain.RunModeExecute
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
		Task: func(runCtx context.Context, submission tui.TaskSubmission) error {
			if len(submission.Content) > maxRootTaskBytes {
				return fmt.Errorf("interactive task exceeds %d bytes", maxRootTaskBytes)
			}
			runInvocation := invocation
			runInvocation.Mode = agentInvocationOnce
			runInvocation.Task = submission.Content
			if submission.Mode == tui.CollaborationPlan {
				runInvocation.RunMode = sessiondomain.RunModePlan
			} else {
				runInvocation.RunMode = sessiondomain.RunModeExecute
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
				err := writeInteractiveMCP(commandCtx, &output, sessionRuntime, strings.EqualFold(arguments, "verbose"))
				return strings.TrimSpace(output.String()), err
			case tui.SlashClear:
				err := sessionRuntime.NewDraft()
				return "Started a new chat", err
			default:
				return "", fmt.Errorf("command /%s is not delegated through the generic handler", spec.Command)
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
		CurrentSession:      func() string { return string(sessionRuntime.CurrentSessionID()) },
		CurrentSessionTitle: func() string { return sessionRuntime.History().Session.Title },
		Rename: func(commandCtx context.Context, title string) (string, error) {
			_, normalized, renameErr := sessionRuntime.RenameCurrent(commandCtx, title)
			if renameErr != nil {
				return "", renameErr
			}
			return "Session renamed to " + normalized, nil
		},
		Delete: func(commandCtx context.Context) (string, error) {
			deleted, deleteErr := sessionRuntime.DeleteCurrent(commandCtx)
			if deleteErr != nil {
				return "", deleteErr
			}
			if deleted == "" {
				return "Draft session discarded", nil
			}
			return "Session deleted: " + string(deleted), nil
		},
		Compact: func(commandCtx context.Context) (string, error) {
			return runner.compactInteractiveSession(commandCtx, configured, sessionRuntime)
		},
		Skills: func(commandCtx context.Context) ([]tui.SkillOption, error) {
			extension, extensionErr := sessionRuntime.EnsureExtension()
			if extensionErr != nil {
				return nil, extensionErr
			}
			runtime, ok := extension.(*extensionruntime.Runtime)
			if !ok || runtime.Skills() == nil {
				return nil, errors.New("Skill catalog is unavailable")
			}
			entries := runtime.Skills().Index()
			options := make([]tui.SkillOption, 0, len(entries))
			for _, entry := range entries {
				options = append(options, tui.SkillOption{Name: entry.Name, Description: entry.Description, Source: string(entry.Source), Enabled: entry.Enabled})
			}
			return options, commandCtx.Err()
		},
		SetSkill: func(commandCtx context.Context, name string, enabled bool) error {
			extension, extensionErr := sessionRuntime.EnsureExtension()
			if extensionErr != nil {
				return extensionErr
			}
			runtime, ok := extension.(*extensionruntime.Runtime)
			if !ok {
				return errors.New("Skill catalog is unavailable")
			}
			if err := runtime.SetSkillEnabled(name, enabled); err != nil {
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

func (runner *agentController) compactInteractiveSession(ctx context.Context, configured config.Config, sessionRuntime *sessiondomain.SessionRuntime) (string, error) {
	if sessionRuntime == nil {
		return "", errors.New("Session runtime is unavailable")
	}
	projection, err := sessiondomain.ProjectMessages(sessionRuntime.History().Items)
	if err != nil {
		return "", err
	}
	if len(projection.Messages) == 0 || len(projection.SourceSequences) != len(projection.Messages) {
		return "", errors.New("There is no conversation to compact")
	}
	providerName := configured.DefaultProvider
	provider, ok := configured.Providers[providerName]
	if !ok {
		return "", fmt.Errorf("default Provider %q is not configured", providerName)
	}
	createClient := runner.runtime.llmClientFactory
	if createClient == nil {
		createClient = defaultLLMClientFactory
	}
	client, err := createClient(providerName, provider)
	if err != nil {
		return "", err
	}
	messages := make([]llm.Message, 0, len(projection.Messages)+2)
	messages = append(messages, llm.SystemMessage("You compact an Amadeus coding-agent session. Produce a concise but complete handoff summary that preserves user intent, decisions, files changed, commands and test results, unresolved work, constraints, and information needed to continue. Return only the summary in Markdown. Do not call tools."))
	messages = append(messages, projection.Messages...)
	messages = append(messages, llm.UserMessage("Compact the conversation above into the canonical continuation summary now."))
	maxOutputTokens := provider.MaxOutputTokens
	if maxOutputTokens <= 0 || maxOutputTokens > 4096 {
		maxOutputTokens = 4096
	}
	response, err := client.Complete(ctx, llm.Request{
		Model: provider.Model, Messages: messages, Temperature: 0,
		MaxOutputTokens: maxOutputTokens,
	})
	if err != nil {
		return "", fmt.Errorf("generate conversation summary: %w", err)
	}
	summary := strings.TrimSpace(response.Message.Content)
	if summary == "" || len(response.Message.ToolCalls) > 0 {
		return "", errors.New("compaction model returned no usable summary")
	}
	encodedSource, err := json.Marshal(projection.Messages)
	if err != nil {
		return "", fmt.Errorf("encode compaction source: %w", err)
	}
	digest := sha256.Sum256(encodedSource)
	payload, err := sessiondomain.EncodePayload(sessiondomain.ContextCompactionPayload{
		Summary:                summary,
		ReplacementHistory:     []sessiondomain.CompactionHistoryItem{{Role: llm.RoleAssistant, Content: summary}},
		CoveredThroughSequence: projection.SourceSequences[len(projection.SourceSequences)-1],
		SourceHash:             hex.EncodeToString(digest[:]), Provider: providerName, Model: provider.Model,
	})
	if err != nil {
		return "", err
	}
	items, err := sessionRuntime.AppendStandalone(context.WithoutCancel(ctx), sessiondomain.AppendItem{
		ID: sessiondomain.RolloutItemID(runner.runtimeID("item")), Kind: sessiondomain.RolloutContextCompaction,
		Payload: payload, CreatedAt: runner.runtimeNow(),
	})
	if err != nil {
		return "", err
	}
	if len(items) != 1 {
		return "", errors.New("compaction did not persist exactly one rollout item")
	}
	if _, err := sessiondomain.DecodeContextCompaction(items[0]); err != nil {
		return "", fmt.Errorf("validate persisted compaction: %w", err)
	}
	return "Conversation compacted", nil
}

func writeInteractiveSkills(ctx context.Context, writer io.Writer, sessionRuntime *sessiondomain.SessionRuntime) error {
	extension, err := sessionRuntime.EnsureExtension()
	if err != nil {
		return err
	}
	runtime, ok := extension.(*extensionruntime.Runtime)
	if !ok || runtime.Skills() == nil {
		return errors.New("Skill catalog is unavailable")
	}
	entries := runtime.Skills().Index()
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

func writeInteractiveMCP(ctx context.Context, writer io.Writer, sessionRuntime *sessiondomain.SessionRuntime, verbose bool) error {
	extension, err := sessionRuntime.EnsureExtension()
	if err != nil {
		return err
	}
	runtime, ok := extension.(*extensionruntime.Runtime)
	if !ok || runtime.MCP() == nil {
		return errors.New("MCP manager is unavailable")
	}
	servers := runtime.MCP().EnabledServers()
	sort.Strings(servers)
	if len(servers) == 0 {
		_, err = fmt.Fprintln(writer, "No MCP servers configured.")
		return err
	}
	bindings := runtime.MCP().BindingSnapshot()
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
		tools, listErr := runtime.MCP().ListTools(ctx, server)
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
	for _, spec := range builtin.MVPSpecs() {
		if _, err := fmt.Fprintf(writer, "%s (%s)\n", spec.Name, spec.SideEffect); err != nil {
			return err
		}
	}
	return nil
}
