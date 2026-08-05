package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/react"
	bootstrap "github.com/Godric-W/Amadeus/internal/app/bootstrap"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/buildinfo"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/instruction"
	interfacecli "github.com/Godric-W/Amadeus/internal/interface/cli"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/render"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
	"github.com/Godric-W/Amadeus/internal/tool/builtin"
	"github.com/spf13/cobra"
)

const envXDGStateHome = "XDG_STATE_HOME"

var agentRunSequence atomic.Uint64

type auditSinkFactory func() (audit.Sink, io.Closer, error)

type codingAgentCommand struct {
	command            *cobra.Command
	flags              *configFlags
	runtime            commandRuntime
	coordinator        *sessiondomain.Coordinator
	sessionCloser      io.Closer
	coordinatorProject string
}

func defaultAgentCommandFactory(command *cobra.Command, flags *configFlags, runtime commandRuntime) (agentCommand, error) {
	if command == nil {
		return nil, errors.New("Coding Agent Cobra command is nil")
	}
	if flags == nil {
		return nil, errors.New("Coding Agent config flags are nil")
	}
	return &codingAgentCommand{command: command, flags: flags, runtime: runtime}, nil
}

func (runner *codingAgentCommand) Run(ctx context.Context, invocation agentInvocation) error {
	if runner == nil || runner.command == nil || runner.flags == nil {
		return errors.New("Coding Agent command is nil")
	}
	if ctx == nil {
		return errors.New("Coding Agent context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	defer runner.closeSessionStore()
	switch invocation.Mode {
	case agentInvocationInteractive:
		return runner.runInteractive(ctx, invocation)
	case agentInvocationOnce:
		if invocation.SessionMode == sessionStartSelect {
			return errors.New("--resume without a session ID requires interactive terminal mode")
		}
		if err := runner.prepareSession(ctx, invocation, nil); err != nil {
			return err
		}
		runCtx, cancel, err := runner.newRunContext(ctx)
		if err != nil {
			return err
		}
		defer cancel()
		return runner.runOnce(runCtx, invocation)
	default:
		return fmt.Errorf("unsupported Coding Agent invocation mode %q", invocation.Mode)
	}
}

func (runner *codingAgentCommand) ensureCoordinator(ctx context.Context, invocation agentInvocation) (*sessiondomain.Coordinator, error) {
	if runner.coordinator != nil {
		if runner.coordinatorProject != invocation.Project.Path() {
			return nil, errors.New("Coding Agent coordinator project changed")
		}
		return runner.coordinator, nil
	}
	if runner.runtime.rootErr != nil {
		return nil, fmt.Errorf("resolve Amadeus root for session store: %w", runner.runtime.rootErr)
	}
	factory := runner.runtime.sessionStoreFactory
	if factory == nil {
		factory = defaultSessionStoreFactory
	}
	store, closer, err := factory(ctx, runner.runtime.amadeusRoot)
	if err != nil {
		return nil, err
	}
	idFactory := runner.runtime.persistentIDFactory
	if idFactory == nil {
		idFactory = nextPersistentID
	}
	baseIDFactory := idFactory
	idFactory = func(kind string) string {
		if kind == "run" && runner.runtime.runIDFactory != nil {
			return runner.runtime.runIDFactory()
		}
		return baseIDFactory(kind)
	}
	clock := runner.runtime.now
	if clock == nil {
		clock = time.Now
	}
	coordinator, err := sessiondomain.NewCoordinator(store, invocation.Project.Path(), filepath.Base(invocation.Project.Path()), sessiondomain.CoordinatorOptions{IDFactory: idFactory, Clock: clock})
	if err != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return nil, err
	}
	runner.coordinator = coordinator
	runner.sessionCloser = closer
	runner.coordinatorProject = invocation.Project.Path()
	return coordinator, nil
}

func (runner *codingAgentCommand) closeSessionStore() {
	if runner.sessionCloser != nil {
		_ = runner.sessionCloser.Close()
	}
	runner.sessionCloser = nil
	runner.coordinator = nil
	runner.coordinatorProject = ""
}

func (runner *codingAgentCommand) runInteractive(ctx context.Context, invocation agentInvocation) error {
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

func (runner *codingAgentCommand) runPlainInteractive(ctx context.Context, invocation agentInvocation, reader *bufio.Reader, capabilities tui.TerminalCapabilities) error {
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

func (runner *codingAgentCommand) runFullscreenInteractive(ctx context.Context, invocation agentInvocation, capabilities tui.TerminalCapabilities) error {
	configured, _, err := loadEffectiveConfig(runner.command, runner.flags, runner.runtime)
	if err != nil {
		return err
	}
	if err := config.Validate(configured); err != nil {
		return err
	}
	provider := configured.Providers[configured.DefaultProvider]
	coordinator, err := runner.ensureCoordinator(ctx, invocation)
	if err != nil {
		return err
	}
	sessionID := string(coordinator.CurrentSessionID())
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
			sessions, listErr := coordinator.ListSessions(commandCtx)
			if listErr != nil {
				return nil, listErr
			}
			options := make([]tui.SessionOption, 0, len(sessions))
			for _, conversation := range sessions {
				options = append(options, tui.SessionOption{ID: string(conversation.ID), Title: conversation.Title, Current: conversation.ID == coordinator.CurrentSessionID()})
			}
			return options, nil
		},
		Resume: func(commandCtx context.Context, id string) (string, error) {
			conversation, resumeErr := coordinator.Resume(commandCtx, sessiondomain.ConversationSessionID(id))
			if resumeErr != nil {
				return "", resumeErr
			}
			return fmt.Sprintf("Session resumed: %s (%s)", conversation.ID, conversation.Title), nil
		},
		CurrentSession: func() string { return string(coordinator.CurrentSessionID()) },
	})
	if err != nil {
		return err
	}
	return application.Run(ctx)
}

func (runner *codingAgentCommand) writeInteractiveStatus(ctx context.Context, invocation agentInvocation) error {
	coordinator, err := runner.ensureCoordinator(ctx, invocation)
	if err != nil {
		return err
	}
	current := coordinator.CurrentSessionID()
	if current == "" {
		current = "draft"
	}
	_, err = fmt.Fprintf(invocation.ErrorOutput, "status: project=%s session=%s\n", invocation.Project.Path(), current)
	return err
}

func writeInteractiveTools(writer io.Writer) error {
	for _, spec := range builtin.MVPSpecs() {
		if _, err := fmt.Fprintf(writer, "%s (%s)\n", spec.Name, spec.SideEffect); err != nil {
			return err
		}
	}
	return nil
}

func (runner *codingAgentCommand) prepareSession(ctx context.Context, invocation agentInvocation, reader *bufio.Reader) error {
	switch invocation.SessionMode {
	case "", sessionStartDraft:
		return nil
	case sessionStartContinue:
		coordinator, err := runner.ensureCoordinator(ctx, invocation)
		if err != nil {
			return err
		}
		conversation, err := coordinator.Continue(ctx)
		if errors.Is(err, sessiondomain.ErrNotFound) {
			fmt.Fprintln(invocation.ErrorOutput, "session: no previous session; using a new draft")
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(invocation.ErrorOutput, "session: continued %s (%s)\n", conversation.ID, conversation.Title)
		return nil
	case sessionStartResume:
		coordinator, err := runner.ensureCoordinator(ctx, invocation)
		if err != nil {
			return err
		}
		conversation, err := coordinator.Resume(ctx, invocation.SessionID)
		if err != nil {
			return err
		}
		fmt.Fprintf(invocation.ErrorOutput, "session: resumed %s (%s)\n", conversation.ID, conversation.Title)
		return nil
	case sessionStartSelect:
		if reader == nil {
			return errors.New("session selector requires interactive input")
		}
		return runner.selectSession(ctx, invocation, reader)
	default:
		return fmt.Errorf("unsupported session start mode %q", invocation.SessionMode)
	}
}

func (runner *codingAgentCommand) selectSession(ctx context.Context, invocation agentInvocation, reader *bufio.Reader) error {
	coordinator, err := runner.ensureCoordinator(ctx, invocation)
	if err != nil {
		return err
	}
	sessions, err := coordinator.ListSessions(ctx)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Fprintln(invocation.ErrorOutput, "session: no previous sessions; current conversation unchanged")
		return nil
	}
	fmt.Fprintln(invocation.ErrorOutput, "Select a session (Esc or empty input cancels):")
	for index, conversation := range sessions {
		marker := " "
		if conversation.ID == coordinator.CurrentSessionID() {
			marker = "*"
		}
		fmt.Fprintf(invocation.ErrorOutput, "%s %d) %s  %s  %s\n", marker, index+1, conversation.ID, conversation.Title, conversation.UpdatedAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprint(invocation.ErrorOutput, "resume> ")
	line, readErr := reader.ReadString('\n')
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return fmt.Errorf("read session selection: %w", readErr)
	}
	value := strings.TrimSpace(line)
	if value == "" || value == "\x1b" {
		fmt.Fprintln(invocation.ErrorOutput, "session: selection cancelled")
		return nil
	}
	selected := sessiondomain.ConversationSessionID(value)
	if index, parseErr := strconv.Atoi(value); parseErr == nil {
		if index < 1 || index > len(sessions) {
			return fmt.Errorf("session selection %d is out of range", index)
		}
		selected = sessions[index-1].ID
	}
	conversation, err := coordinator.Resume(ctx, selected)
	if err != nil {
		return err
	}
	fmt.Fprintf(invocation.ErrorOutput, "session: resumed %s (%s)\n", conversation.ID, conversation.Title)
	return nil
}

func (runner *codingAgentCommand) newRunContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	factory := runner.runtime.agentContextFactory
	if factory == nil {
		factory = interruptibleTurnContext
	}
	ctx, cancel := factory(parent)
	if ctx == nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		return nil, nil, errors.New("Coding Agent Run context factory returned nil")
	}
	return ctx, cancel, nil
}

func (runner *codingAgentCommand) runOnce(ctx context.Context, invocation agentInvocation) (runErr error) {
	executionMode, objective, err := parseAgentTask(invocation.Task)
	if err != nil {
		return err
	}
	invocation.Task = objective
	configured, _, err := loadEffectiveConfig(runner.command, runner.flags, runner.runtime)
	if err != nil {
		return err
	}
	if err := config.Validate(configured); err != nil {
		return err
	}
	coordinator, err := runner.ensureCoordinator(ctx, invocation)
	if err != nil {
		return err
	}
	provider := configured.Providers[configured.DefaultProvider]
	started, err := coordinator.BeginRun(context.WithoutCancel(ctx), invocation.Task, sessiondomain.RunMetadata{
		Provider: configured.DefaultProvider, Model: provider.Model, APIMode: string(provider.API), Dialect: string(provider.Dialect),
		ExecutionMode: sessiondomain.ExecutionMode(executionMode),
	})
	if err != nil {
		return err
	}
	ctx = event.WithMetadata(ctx, event.Metadata{
		SessionID: string(started.Records.Session.ID),
		RunID:     string(started.Records.Run.ID),
	})
	finished := false
	defer func() {
		if finished {
			return
		}
		status := sessiondomain.RunFailed
		reason := "run setup or execution failed"
		if errors.Is(ctx.Err(), context.Canceled) {
			status = sessiondomain.RunInterrupted
			reason = "user cancelled"
		} else if runErr != nil {
			reason = foldSummary(runErr.Error())
		}
		fallbackContext, _ := sessiondomain.EncodePreviousWork(sessiondomain.PreviousWork{Objective: invocation.Task, Status: string(status), StopReason: reason, LastError: reason, PendingWork: []string{"Re-plan from the current workspace state."}})
		_, finishErr := coordinator.FinishRun(context.WithoutCancel(ctx), started, status, reason, "", nil, fallbackContext)
		if finishErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("persist failed Run terminal state: %w", finishErr))
		}
	}()

	detectTerminal := runner.runtime.terminalDetector
	if detectTerminal == nil {
		detectTerminal = isTerminalInput
	}
	var renderer event.Sink
	inlineMode := false
	if invocation.EventSink != nil || invocation.Approvals != nil {
		if invocation.EventSink == nil || invocation.Approvals == nil {
			return errors.New("Coding Agent external TUI requires both event sink and approval handler")
		}
		renderer = invocation.EventSink
	} else {
		capabilities := tui.DetectTerminalCapabilitiesWithOptions(invocation.Input, invocation.Output, tui.TerminalCapabilityOptions{
			IsTerminal: func(input io.Reader) bool { return detectTerminal(input) }, ForcePlain: invocation.Plain,
		})
		if capabilities.TTY && !capabilities.Plain {
			renderer, err = tui.NewInlineRenderer(invocation.Output, invocation.ErrorOutput)
			inlineMode = true
		} else {
			renderer, err = render.NewAgentRenderer(invocation.Output, invocation.ErrorOutput)
		}
		if err != nil {
			return err
		}
	}
	var approvals policy.ApprovalHandler
	if invocation.Approvals != nil {
		approvals = invocation.Approvals
	} else if inlineMode {
		approvals, err = tui.NewInlineApprovalPrompt(tui.InlineApprovalPromptOptions{Input: invocation.Input, Output: invocation.ErrorOutput, IsTerminal: func(input io.Reader) bool { return detectTerminal(input) }})
	} else {
		approvals, err = interfacecli.NewTerminalApprovalHandler(interfacecli.TerminalApprovalOptions{Input: invocation.Input, Output: invocation.ErrorOutput, IsTerminal: func(input io.Reader) bool { return detectTerminal(input) }})
	}
	if err != nil {
		return err
	}
	eventHub, err := event.NewHub(renderer)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := eventHub.Close(); closeErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close Coding Agent event hub: %w", closeErr))
		}
	}()
	auditFactory := runner.runtime.auditSinkFactory
	if auditFactory == nil {
		auditFactory = defaultAuditSinkFactory(runner.runtime.lookupEnv, os.UserHomeDir)
	}
	auditSink, auditCloser, err := auditFactory()
	if err != nil {
		return err
	}
	if auditSink == nil {
		return errors.New("Coding Agent audit factory returned nil sink")
	}
	if auditCloser != nil {
		defer func() {
			if closeErr := auditCloser.Close(); closeErr != nil {
				runErr = errors.Join(runErr, fmt.Errorf("close Coding Agent audit sink: %w", closeErr))
			}
		}()
	}
	postWriteHooks := append([]react.PostExecutionHook(nil), runner.runtime.postWriteHooks...)

	options := bootstrap.AgentOptions{
		SnapshotRunID:    string(started.Records.Run.ID),
		UserSkillRoot:    runner.runtime.amadeusRoot,
		UserMCPRoot:      runner.runtime.amadeusRoot,
		MCPClientFactory: runner.runtime.mcpClientFactory,
		WebFetcher:       runner.runtime.webFetcher,
		WebSearch:        runner.runtime.webSearch,
		PostWriteHooks:   postWriteHooks,
	}
	if runner.runtime.llmClientFactory != nil {
		options.ClientFactory = func(providerName string, provider config.ProviderConfig) (client llm.Client, err error) {
			return runner.runtime.llmClientFactory(providerName, provider)
		}
	}
	agent, err := bootstrap.NewAgentWithOptions(configured, invocation.Project, eventHub, approvals, auditSink, options)
	if err != nil {
		return err
	}
	defer agent.Processes.CloseOwner(string(started.Records.Run.ID))
	defer func() {
		if closeErr := agent.Close(); closeErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close Coding Agent MCP clients: %w", closeErr))
		}
	}()
	if runner.runtime.rootErr != nil {
		return fmt.Errorf("resolve Amadeus root for user instructions: %w", runner.runtime.rootErr)
	}
	userLoader, err := instruction.NewUserLoader(runner.runtime.amadeusRoot, instruction.UserLoaderOptions{})
	if err != nil {
		return err
	}
	projectLoader, err := instruction.NewProjectLoader(invocation.Project, instruction.ProjectLoaderOptions{})
	if err != nil {
		return err
	}
	resolver, err := instruction.NewLayeredResolver(userLoader, projectLoader)
	if err != nil {
		return err
	}
	request, err := instruction.NewResolveRequest(invocation.Project, ".", instruction.TargetCommandCWD)
	if err != nil {
		return err
	}
	resolved, err := resolver.Resolve(ctx, request)
	if err != nil {
		return err
	}
	conversationMessages := started.PriorMessages
	var existingSummary *sessiondomain.ConversationSummary
	if summary, summaryErr := coordinator.LatestSummary(ctx, started.Records.Session.ID); summaryErr == nil {
		existingSummary = &summary
		conversationMessages = messagesAfterSequence(conversationMessages, summary.ToMessageSequence)
	} else if !errors.Is(summaryErr, sessiondomain.ErrNotFound) {
		return summaryErr
	}
	conversation := persistentConversation(conversationMessages)
	interrupted, err := runner.previousWork(ctx, started, invocation.Project)
	if err != nil {
		return err
	}
	envelope, err := agent.ContextBuilder.Build(ctx, agentcontext.BuildInput{
		Prompt: agent.AgentPrompt, InstructionRequest: request, Instructions: resolved,
		Conversation: conversation, ConversationSummary: summaryContent(existingSummary), PreviousWork: interrupted,
		Budget: agentcontext.DefaultBudget(configured.Agent.MaxInputTokens, configured.Agent.MaxOutputTokens),
		Task:   invocation.Task, Tools: agent.AvailableTools(), SkillIndex: agent.SkillIndex(),
	})
	if err != nil {
		return err
	}
	if err := runner.persistCompaction(ctx, coordinator, started, conversationMessages, existingSummary, envelope, configured.DefaultProvider, provider.Model); err != nil {
		return err
	}
	if executionMode == agentExecutionReAct {
		var persisted bool
		persisted, err = runner.executeReactorRun(ctx, coordinator, started, invocation, configured, agent, envelope)
		finished = persisted
		return err
	}

	goal := plan.Goal{Objective: invocation.Task}
	state := plan.NewRun(plan.RunID(started.Records.Run.ID), goal, plan.NewPlanGraph(), configuredAgentBudget(configured.Agent))
	controller := agent.PlanController
	if controller == nil {
		return fmt.Errorf("Coding Agent %s engine is not configured", executionMode)
	}
	result, planErr := controller.Run(ctx, plan.PlanRunInput{
		State: state,
		WorkspaceSnapshot: func(snapshotCtx context.Context) (string, error) {
			workspace := revalidatePreviousWorkspace(snapshotCtx, invocation.Project, nil)
			encoded, encodeErr := json.Marshal(workspace)
			if encodeErr != nil {
				return "", fmt.Errorf("encode planning workspace: %w", encodeErr)
			}
			return string(encoded), nil
		},
		Messages: envelope.Messages, AvailableTools: envelope.AvailableTools,
	})
	if _, snapshotErr := agent.SnapshotRun.Complete(context.WithoutCancel(ctx)); snapshotErr != nil {
		planErr = errors.Join(planErr, fmt.Errorf("capture Run snapshot after execution: %w", snapshotErr))
	}
	status, stopReason := persistentRunOutcome(result, planErr, ctx.Err())
	usageJSON, marshalErr := json.Marshal(result.State.Budget)
	if marshalErr != nil {
		return errors.Join(planErr, fmt.Errorf("encode persistent Run usage: %w", marshalErr))
	}
	interruptedContext, contextErr := interruptedContextFromResult(status, stopReason, invocation.Task, result)
	if contextErr != nil {
		return errors.Join(planErr, contextErr)
	}
	assistantContent := ""
	if status == sessiondomain.RunCompleted {
		assistantContent = completedAssistantContent(result)
	}
	if _, finishErr := coordinator.FinishRun(context.WithoutCancel(ctx), started, status, stopReason, assistantContent, usageJSON, interruptedContext); finishErr != nil {
		return errors.Join(planErr, finishErr)
	}
	finished = true
	if planErr != nil {
		return planErr
	}
	outcome, code, err := classifyRunResult(result)
	if err != nil {
		return err
	}
	summary := formatRunSummary(outcome, result)
	fmt.Fprintln(invocation.ErrorOutput, summary)
	if code != exitCodeSuccess {
		return &commandExitError{code: code, message: summary, reported: true}
	}
	return nil
}

func (runner *codingAgentCommand) executeReactorRun(ctx context.Context, coordinator *sessiondomain.Coordinator, started sessiondomain.StartedRun, invocation agentInvocation, configured config.Config, agent *bootstrap.Agent, envelope agentcontext.Envelope) (bool, error) {
	if err := agent.Events.Publish(ctx, event.RunStarted{}); err != nil {
		return false, fmt.Errorf("publish Reactor Run started: %w", err)
	}
	if err := agent.Events.Publish(ctx, event.RunStatusChanged{Entity: "run", EntityID: string(started.Records.Run.ID), From: "", To: "running"}); err != nil {
		return false, fmt.Errorf("publish Reactor Run status: %w", err)
	}
	result, runErr := agent.Runner.Run(ctx, react.Request{
		RunID: string(started.Records.Run.ID), Goal: invocation.Task,
		Messages: envelope.Messages, AvailableTools: envelope.AvailableTools,
		Budget: configuredReactorBudget(configured.Agent),
	})
	if _, snapshotErr := agent.SnapshotRun.Complete(context.WithoutCancel(ctx)); snapshotErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("capture Run snapshot after execution: %w", snapshotErr))
	}
	status, stopReason := persistentReactorOutcome(result, runErr, ctx.Err())
	if publishErr := agent.Events.Publish(context.WithoutCancel(ctx), event.RunCompleted{
		Status: string(status), StopReason: string(result.StopReason), Reason: reactorRunEventReason(status, stopReason, result.Reason),
	}); publishErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("publish Reactor Run completed: %w", publishErr))
	}
	usageJSON, marshalErr := json.Marshal(struct {
		Budget react.BudgetState `json:"budget"`
		Usage  llm.Usage         `json:"usage"`
	}{Budget: result.Budget, Usage: result.Usage})
	if marshalErr != nil {
		return false, errors.Join(runErr, fmt.Errorf("encode persistent Reactor usage: %w", marshalErr))
	}
	previousWork, contextErr := previousWorkFromReactor(status, stopReason, invocation.Task, result)
	if contextErr != nil {
		return false, errors.Join(runErr, contextErr)
	}
	assistantContent := ""
	if status == sessiondomain.RunCompleted && result.FinalMessage != nil {
		assistantContent = strings.TrimSpace(result.FinalMessage.Content)
	}
	if _, finishErr := coordinator.FinishRun(context.WithoutCancel(ctx), started, status, stopReason, assistantContent, usageJSON, previousWork); finishErr != nil {
		return false, errors.Join(runErr, finishErr)
	}
	if runErr != nil {
		return true, runErr
	}
	outcome, code := classifyReactorResult(result)
	summary := formatReactorSummary(outcome, result)
	fmt.Fprintln(invocation.ErrorOutput, summary)
	if code != exitCodeSuccess {
		return true, &commandExitError{code: code, message: summary, reported: true}
	}
	return true, nil
}

func (runner *codingAgentCommand) runtimeID(kind string) string {
	factory := runner.runtime.persistentIDFactory
	if factory == nil {
		factory = nextPersistentID
	}
	return factory(kind)
}

func (runner *codingAgentCommand) runtimeNow() time.Time {
	clock := runner.runtime.now
	if clock == nil {
		clock = time.Now
	}
	return clock().UTC()
}

func persistentConversation(messages []sessiondomain.Message) []llm.Message {
	result := make([]llm.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == sessiondomain.MessageUser {
			result = append(result, llm.UserMessage(message.Content))
		} else if message.Role == sessiondomain.MessageAssistant {
			result = append(result, llm.AssistantMessage(message.Content))
		}
	}
	return result
}

func messagesAfterSequence(messages []sessiondomain.Message, sequence int64) []sessiondomain.Message {
	for index, message := range messages {
		if message.Sequence > sequence {
			return messages[index:]
		}
	}
	return nil
}

func summaryContent(summary *sessiondomain.ConversationSummary) string {
	if summary == nil {
		return ""
	}
	return summary.Content
}

func (runner *codingAgentCommand) persistCompaction(ctx context.Context, coordinator *sessiondomain.Coordinator, started sessiondomain.StartedRun, recent []sessiondomain.Message, existing *sessiondomain.ConversationSummary, envelope agentcontext.Envelope, provider, model string) error {
	if envelope.Compaction == nil || envelope.Compaction.CoveredMessages < 1 || envelope.Compaction.CoveredMessages > len(recent) {
		return nil
	}
	coveredTo := recent[envelope.Compaction.CoveredMessages-1].Sequence
	allCovered := make([]sessiondomain.Message, 0, len(started.PriorMessages))
	for _, message := range started.PriorMessages {
		if message.Sequence <= coveredTo {
			allCovered = append(allCovered, message)
		}
	}
	sourceHash, err := sessiondomain.ConversationSourceHash(allCovered)
	if err != nil {
		return err
	}
	content := envelope.Compaction.Summary
	if existing != nil {
		content = existing.Content + "\n" + content
	}
	summary, err := sessiondomain.NewConversationSummary(
		sessiondomain.SummaryID(runner.runtimeID("summary")), started.Records.Session.ID,
		allCovered[0].Sequence, coveredTo, content, sourceHash, provider, model, runner.runtimeNow(),
	)
	if err != nil {
		return err
	}
	_, err = coordinator.AppendSummary(context.WithoutCancel(ctx), summary)
	return err
}

func (runner *codingAgentCommand) previousWork(ctx context.Context, started sessiondomain.StartedRun, root project.Root) (*agentcontext.PreviousWork, error) {
	if started.PreviousRun == nil {
		return nil, nil
	}
	payload, err := sessiondomain.DecodePreviousWork(started.PreviousRun.InterruptedContextJSON)
	if err != nil {
		return nil, err
	}
	work := &agentcontext.PreviousWork{
		RunID: string(started.PreviousRun.ID), Objective: payload.Objective, StopReason: payload.StopReason,
		RelevantPaths: append([]string(nil), payload.RelevantPaths...), PendingWork: append([]string(nil), payload.PendingWork...),
		Usage:     append(json.RawMessage(nil), payload.Usage...),
		Workspace: revalidatePreviousWorkspace(ctx, root, payload.RelevantPaths),
	}
	work.CompletedWork = append(work.CompletedWork, payload.CompletedWork...)
	work.Evidence = append(work.Evidence, payload.Evidence...)
	return work, nil
}

func persistentRunOutcome(result plan.PlanRunResult, runErr, contextErr error) (sessiondomain.RunStatus, string) {
	if errors.Is(contextErr, context.Canceled) || result.State.Status == plan.RunStatusCancelled {
		return sessiondomain.RunInterrupted, "user cancelled"
	}
	if runErr != nil {
		return sessiondomain.RunFailed, foldSummary(runErr.Error())
	}
	switch result.State.Status {
	case plan.RunStatusCompleted:
		return sessiondomain.RunCompleted, ""
	case plan.RunStatusFailed:
		return sessiondomain.RunFailed, nonEmptyStopReason(result.Reason, string(result.State.StopReason))
	default:
		return sessiondomain.RunFailed, "unsupported terminal engine status"
	}
}

func persistentReactorOutcome(result react.Result, runErr, contextErr error) (sessiondomain.RunStatus, string) {
	if errors.Is(contextErr, context.Canceled) || result.StopReason == react.StopInterrupted {
		return sessiondomain.RunInterrupted, "user cancelled"
	}
	if runErr != nil {
		return sessiondomain.RunFailed, foldSummary(runErr.Error())
	}
	if result.StopReason == react.StopCompleted {
		return sessiondomain.RunCompleted, ""
	}
	return sessiondomain.RunFailed, nonEmptyStopReason(result.Reason, string(result.StopReason))
}

func classifyReactorResult(result react.Result) (runOutcome, int) {
	switch result.StopReason {
	case react.StopCompleted:
		return runOutcomeCompleted, exitCodeSuccess
	case react.StopInterrupted:
		return runOutcomeCancelled, exitCodeCancelled
	case react.StopBlocked, react.StopStalled:
		return runOutcomePartial, exitCodePartial
	default:
		return runOutcomeFailed, exitCodeFailure
	}
}

func formatReactorSummary(outcome runOutcome, result react.Result) string {
	parts := []string{"result: " + string(outcome)}
	if result.StopReason != "" {
		parts = append(parts, "stop_reason="+string(result.StopReason))
	}
	if reason := strings.TrimSpace(result.Reason); reason != "" {
		parts = append(parts, "reason="+foldSummary(reason))
	}
	return strings.Join(parts, " ")
}

func previousWorkFromReactor(status sessiondomain.RunStatus, stopReason, objective string, result react.Result) (json.RawMessage, error) {
	if status == sessiondomain.RunCompleted {
		return nil, nil
	}
	payload := sessiondomain.PreviousWork{
		Objective: objective, Status: string(status), StopReason: stopReason,
		LastError: nonEmptyStopReason(result.Reason, stopReason),
	}
	for _, evidence := range result.Evidence {
		summary := foldSummary(evidence.Summary)
		if summary != "" {
			payload.Evidence = append(payload.Evidence, summary)
			if evidence.Verified {
				payload.CompletedWork = append(payload.CompletedWork, summary)
			}
		}
		if evidence.Artifact != nil && evidence.Artifact.Path != "" {
			payload.RelevantPaths = append(payload.RelevantPaths, evidence.Artifact.Path)
		}
	}
	payload.PendingWork = []string{"Re-plan the unfinished objective from the current workspace state."}
	usage, _ := json.Marshal(struct {
		Budget react.BudgetState `json:"budget"`
		Usage  llm.Usage         `json:"usage"`
	}{Budget: result.Budget, Usage: result.Usage})
	payload.Usage = usage
	return sessiondomain.EncodePreviousWork(payload)
}

func nonEmptyStopReason(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return foldSummary(value)
		}
	}
	return "run did not complete"
}

func reactorRunEventReason(status sessiondomain.RunStatus, values ...string) string {
	if status == sessiondomain.RunCompleted {
		return ""
	}
	return nonEmptyStopReason(values...)
}

func completedAssistantContent(result plan.PlanRunResult) string {
	if result.FinalMessage != nil && strings.TrimSpace(result.FinalMessage.Content) != "" {
		return result.FinalMessage.Content
	}
	if len(result.State.Graph.Tasks) > 0 && result.State.Graph.Tasks[0].Result != nil {
		return result.State.Graph.Tasks[0].Result.Summary
	}
	return "Task completed."
}

func interruptedContextFromResult(status sessiondomain.RunStatus, stopReason, objective string, result plan.PlanRunResult) (json.RawMessage, error) {
	if status == sessiondomain.RunCompleted {
		return nil, nil
	}
	payload := sessiondomain.PreviousWork{
		Objective: objective, Status: string(status), StopReason: stopReason,
		LastError: nonEmptyStopReason(result.Reason, stopReason),
	}
	for _, task := range result.State.Graph.Tasks {
		summary := nonEmptyStopReason(task.Objective, string(task.ID))
		if task.Status == plan.TaskStatusCompleted {
			payload.CompletedWork = append(payload.CompletedWork, "task "+string(task.ID)+": "+summary)
			continue
		}
		payload.PendingWork = append(payload.PendingWork, "task "+string(task.ID)+": "+summary)
	}
	for _, evidence := range result.State.Evidence {
		payload.Evidence = append(payload.Evidence, foldSummary(evidence.Summary))
		if evidence.Artifact != nil && evidence.Artifact.Path != "" {
			payload.RelevantPaths = append(payload.RelevantPaths, evidence.Artifact.Path)
		}
	}
	if len(payload.PendingWork) == 0 {
		payload.PendingWork = []string{"Re-plan the interrupted objective from current workspace state."}
	}
	usage, _ := json.Marshal(result.State.Budget)
	payload.Usage = usage
	return sessiondomain.EncodePreviousWork(payload)
}

func configuredAgentBudget(agent config.AgentConfig) plan.Budget {
	return plan.Budget{
		MaxIterations:   agent.MaxIterations,
		MaxToolCalls:    agent.MaxToolCalls,
		MaxInputTokens:  agent.MaxInputTokens,
		MaxOutputTokens: agent.MaxOutputTokens,
		MaxDuration:     agent.MaxDuration,
	}
}

func configuredReactorBudget(agent config.AgentConfig) react.BudgetState {
	return react.BudgetState{Budget: react.Budget{
		MaxIterations: agent.MaxIterations, MaxToolCalls: agent.MaxToolCalls,
		MaxInputTokens: agent.MaxInputTokens, MaxOutputTokens: agent.MaxOutputTokens,
		MaxDuration: agent.MaxDuration,
	}}
}

func defaultAuditSinkFactory(lookupEnv config.EnvLookup, userHomeDir func() (string, error)) auditSinkFactory {
	return func() (audit.Sink, io.Closer, error) {
		path, err := resolveAuditPath(lookupEnv, userHomeDir)
		if err != nil {
			return nil, nil, err
		}
		file, err := audit.OpenJSONLFile(path)
		if err != nil {
			return nil, nil, err
		}
		return file, file, nil
	}
}

func resolveAuditPath(lookupEnv config.EnvLookup, userHomeDir func() (string, error)) (string, error) {
	if lookupEnv != nil {
		if stateHome, ok := lookupEnv(envXDGStateHome); ok && strings.TrimSpace(stateHome) != "" {
			return filepath.Join(stateHome, "amadeus", "audit", "audit.jsonl"), nil
		}
	}
	if userHomeDir == nil {
		return "", errors.New("user home resolver is nil")
	}
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home for audit log: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", errors.New("user home for audit log is empty")
	}
	return filepath.Join(home, ".local", "state", "amadeus", "audit", "audit.jsonl"), nil
}

func nextAgentRunID() string {
	return fmt.Sprintf("run-%d-%d", time.Now().UTC().UnixNano(), agentRunSequence.Add(1))
}

var _ agentCommand = (*codingAgentCommand)(nil)
