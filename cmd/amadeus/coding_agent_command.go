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
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	bootstrap "github.com/Godric-W/Amadeus/internal/app/bootstrap"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/instruction"
	interfacecli "github.com/Godric-W/Amadeus/internal/interface/cli"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/render"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
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
	if err := runner.prepareSession(ctx, invocation, reader); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fmt.Fprint(invocation.ErrorOutput, "amadeus> ")
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read interactive task: %w", err)
		}
		if len(line) > maxRootTaskBytes {
			fmt.Fprintf(invocation.ErrorOutput, "error: interactive task exceeds %d bytes\n", maxRootTaskBytes)
		} else {
			task := strings.TrimSpace(line)
			switch task {
			case "":
			case "/help":
				fmt.Fprintln(invocation.ErrorOutput, "commands: /help, /resume, /exit")
			case "/resume":
				if err := runner.selectSession(ctx, invocation, reader); err != nil {
					fmt.Fprintf(invocation.ErrorOutput, "error: %v\n", err)
				}
			case "/exit":
				fmt.Fprintln(invocation.ErrorOutput, "session: closed")
				return nil
			default:
				runInvocation := invocation
				runInvocation.Mode = agentInvocationOnce
				runInvocation.Task = task
				runInvocation.Input = reader
				runCtx, cancel, contextErr := runner.newRunContext(ctx)
				if contextErr != nil {
					return contextErr
				}
				runErr := runner.runOnce(runCtx, runInvocation)
				cancel()
				if runErr != nil && !errorAlreadyReported(runErr) {
					fmt.Fprintf(invocation.ErrorOutput, "error: %v\n", runErr)
				}
			}
		}
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(invocation.ErrorOutput, "session: closed")
			return nil
		}
	}
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
	if strings.TrimSpace(invocation.Task) == "" {
		return errors.New("Coding Agent one-shot task is invalid")
	}

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
	budgetJSON, err := json.Marshal(configuredAgentBudget(configured.Agent))
	if err != nil {
		return fmt.Errorf("encode persistent Run budget: %w", err)
	}
	started, err := coordinator.BeginTask(context.WithoutCancel(ctx), invocation.Task, sessiondomain.RunMetadata{
		Provider: configured.DefaultProvider, Model: provider.Model, APIMode: string(provider.API), Dialect: string(provider.Dialect), BudgetJSON: budgetJSON,
	})
	if err != nil {
		return err
	}
	finished := false
	defer func() {
		if finished {
			return
		}
		status := sessiondomain.RunFailed
		reason := "run setup or execution failed"
		if errors.Is(ctx.Err(), context.Canceled) {
			status = sessiondomain.RunCancelled
			reason = "user cancelled"
		} else if runErr != nil {
			reason = foldSummary(runErr.Error())
		}
		_, finishErr := coordinator.FinishTask(context.WithoutCancel(ctx), started, status, reason, "", nil)
		if finishErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("persist failed Run terminal state: %w", finishErr))
		}
	}()

	renderer, err := render.NewAgentRenderer(invocation.Output, invocation.ErrorOutput)
	if err != nil {
		return err
	}
	detectTerminal := runner.runtime.terminalDetector
	if detectTerminal == nil {
		detectTerminal = isTerminalInput
	}
	approvals, err := interfacecli.NewTerminalApprovalHandler(interfacecli.TerminalApprovalOptions{
		Input: invocation.Input, Output: invocation.ErrorOutput, Enabled: configured.Approval.Enabled, Default: configured.Approval.Default,
		IsTerminal: func(input io.Reader) bool { return detectTerminal(input) },
	})
	if err != nil {
		return err
	}
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

	options := bootstrap.AgentOptions{}
	if runner.runtime.llmClientFactory != nil {
		options.ClientFactory = func(providerName string, provider config.ProviderConfig) (client llm.Client, err error) {
			return runner.runtime.llmClientFactory(providerName, provider)
		}
	}
	agent, err := bootstrap.NewAgentWithOptions(configured, invocation.Project, renderer, approvals, auditSink, options)
	if err != nil {
		return err
	}
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
	startCheckpoint, err := sessiondomain.NewCheckpoint(
		sessiondomain.CheckpointID(runner.runtimeID("checkpoint")), started.Records.Run.ID, 1,
		sessiondomain.CheckpointRunStarted,
		sessiondomain.CheckpointPayloadV1{Objective: invocation.Task, Status: string(sessiondomain.RunRunning), PendingWork: []string{invocation.Task}},
		runner.runtimeNow(),
	)
	if err != nil {
		return err
	}
	if _, err := coordinator.AppendCheckpoint(context.WithoutCancel(ctx), sessiondomain.AppendCheckpointInput{
		Checkpoint: startCheckpoint, Instructions: persistentInstructions(startCheckpoint.ID, resolved),
	}); err != nil {
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
	interrupted, err := runner.interruptedWork(ctx, coordinator, started, resolved, invocation.Project)
	if err != nil {
		return err
	}
	envelope, err := agent.ContextBuilder.Build(ctx, agentcontext.BuildInput{
		Prompt: agent.AgentPrompt, InstructionRequest: request, Instructions: resolved,
		Conversation: conversation, ConversationSummary: summaryContent(existingSummary), InterruptedWork: interrupted,
		Budget: agentcontext.DefaultBudget(configured.Agent.MaxInputTokens, configured.Agent.MaxOutputTokens),
		Task:   invocation.Task, Tools: agent.AvailableTools(),
	})
	if err != nil {
		return err
	}
	if err := runner.persistCompaction(ctx, coordinator, started, conversationMessages, existingSummary, envelope, configured.DefaultProvider, provider.Model); err != nil {
		return err
	}

	goal := engine.Goal{Objective: invocation.Task}
	state := engine.NewRun(engine.RunID(started.Records.Run.ID), goal, engine.NewDirectGraph(goal), configuredAgentBudget(configured.Agent))
	result, engineErr := agent.Engine.Run(ctx, engine.DirectRunInput{
		State: state, Messages: envelope.Messages, AvailableTools: envelope.AvailableTools,
	})
	status, stopReason := persistentRunOutcome(result, engineErr, ctx.Err())
	usageJSON, marshalErr := json.Marshal(result.State.Budget)
	if marshalErr != nil {
		return errors.Join(engineErr, fmt.Errorf("encode persistent Run usage: %w", marshalErr))
	}
	terminalCheckpoint, checkpointErr := checkpointFromResult(
		sessiondomain.CheckpointID(runner.runtimeID("checkpoint")), started.Records.Run.ID, 2,
		status, stopReason, invocation.Task, result, runner.runtimeNow(),
	)
	if checkpointErr != nil {
		return errors.Join(engineErr, checkpointErr)
	}
	if _, checkpointErr = coordinator.AppendCheckpoint(context.WithoutCancel(ctx), sessiondomain.AppendCheckpointInput{
		Checkpoint: terminalCheckpoint, Instructions: persistentInstructions(terminalCheckpoint.ID, resolved),
	}); checkpointErr != nil {
		return errors.Join(engineErr, checkpointErr)
	}
	assistantContent := ""
	if status == sessiondomain.RunCompleted {
		assistantContent = completedAssistantContent(result)
	}
	if _, finishErr := coordinator.FinishTask(context.WithoutCancel(ctx), started, status, stopReason, assistantContent, usageJSON); finishErr != nil {
		return errors.Join(engineErr, finishErr)
	}
	finished = true
	if engineErr != nil {
		return engineErr
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

func (runner *codingAgentCommand) persistCompaction(ctx context.Context, coordinator *sessiondomain.Coordinator, started sessiondomain.StartedTurn, recent []sessiondomain.Message, existing *sessiondomain.ConversationSummary, envelope agentcontext.Envelope, provider, model string) error {
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

func persistentInstructions(checkpointID sessiondomain.CheckpointID, resolved instruction.Resolution) []sessiondomain.CheckpointInstruction {
	result := make([]sessiondomain.CheckpointInstruction, len(resolved.Documents))
	for index, document := range resolved.Documents {
		result[index] = sessiondomain.CheckpointInstruction{
			CheckpointID: checkpointID, Precedence: index, Path: document.Path,
			ScopePath: string(document.Scope.Kind) + ":" + document.Scope.Path, ContentHash: document.SHA256,
		}
	}
	return result
}

func (runner *codingAgentCommand) interruptedWork(ctx context.Context, coordinator *sessiondomain.Coordinator, started sessiondomain.StartedTurn, resolved instruction.Resolution, root project.Root) (*agentcontext.InterruptedWork, error) {
	if started.Interrupted == nil {
		return nil, nil
	}
	checkpoint, savedInstructions, err := coordinator.LatestCheckpoint(ctx, started.Interrupted.ID)
	if err != nil {
		return nil, err
	}
	var payload sessiondomain.CheckpointPayloadV1
	if err := json.Unmarshal(checkpoint.PayloadJSON, &payload); err != nil {
		return nil, fmt.Errorf("decode interrupted Run checkpoint: %w", err)
	}
	changes := compareInstructions(savedInstructions, resolved)
	work := &agentcontext.InterruptedWork{
		RunID: string(started.Interrupted.ID), Objective: payload.Objective, StopReason: payload.StopReason,
		RelevantPaths: append([]string(nil), payload.RelevantPaths...), PendingWork: append([]string(nil), payload.PendingWork...),
		Usage: append(json.RawMessage(nil), payload.Usage...), InstructionChanges: changes,
		Workspace: revalidateInterruptedWorkspace(ctx, root, payload.RelevantPaths),
	}
	for _, step := range payload.CompletedSteps {
		work.CompletedSteps = append(work.CompletedSteps, step.Summary)
	}
	for _, evidence := range payload.Evidence {
		work.Evidence = append(work.Evidence, evidence.Summary)
	}
	return work, nil
}

func compareInstructions(saved []sessiondomain.CheckpointInstruction, current instruction.Resolution) []string {
	old := make(map[string]string, len(saved))
	for _, item := range saved {
		old[item.Path+"|"+item.ScopePath] = item.ContentHash
	}
	changes := make([]string, 0)
	for _, document := range current.Documents {
		key := document.Path + "|" + string(document.Scope.Kind) + ":" + document.Scope.Path
		hash, ok := old[key]
		switch {
		case !ok:
			changes = append(changes, "added: "+document.Path)
		case hash != document.SHA256:
			changes = append(changes, "changed: "+document.Path)
		}
		delete(old, key)
	}
	for key := range old {
		changes = append(changes, "removed: "+strings.SplitN(key, "|", 2)[0])
	}
	sort.Strings(changes)
	return changes
}

func persistentRunOutcome(result engine.DirectRunResult, runErr, contextErr error) (sessiondomain.RunStatus, string) {
	if errors.Is(contextErr, context.Canceled) || result.State.Status == engine.RunStatusCancelled {
		return sessiondomain.RunCancelled, "user cancelled"
	}
	if runErr != nil {
		return sessiondomain.RunFailed, foldSummary(runErr.Error())
	}
	switch result.State.Status {
	case engine.RunStatusCompleted:
		return sessiondomain.RunCompleted, ""
	case engine.RunStatusPlanning:
		return sessiondomain.RunNeedsPlan, nonEmptyStopReason(result.Reason, "planning required")
	case engine.RunStatusSuspended:
		return sessiondomain.RunPartial, nonEmptyStopReason(result.Reason, "user input required")
	case engine.RunStatusFailed:
		return sessiondomain.RunFailed, nonEmptyStopReason(result.Reason, string(result.State.StopReason))
	default:
		return sessiondomain.RunFailed, "unsupported terminal engine status"
	}
}

func nonEmptyStopReason(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return foldSummary(value)
		}
	}
	return "run did not complete"
}

func completedAssistantContent(result engine.DirectRunResult) string {
	if result.FinalMessage != nil && strings.TrimSpace(result.FinalMessage.Content) != "" {
		return result.FinalMessage.Content
	}
	if len(result.State.Graph.Tasks) > 0 && result.State.Graph.Tasks[0].Result != nil {
		return result.State.Graph.Tasks[0].Result.Summary
	}
	return "Task completed."
}

func checkpointFromResult(id sessiondomain.CheckpointID, runID sessiondomain.RunID, sequence int64, status sessiondomain.RunStatus, stopReason, objective string, result engine.DirectRunResult, at time.Time) (sessiondomain.Checkpoint, error) {
	payload := sessiondomain.CheckpointPayloadV1{Objective: objective, Status: string(status), StopReason: stopReason}
	for _, step := range result.Steps {
		if step.Status == engine.StepStatusCompleted {
			payload.CompletedSteps = append(payload.CompletedSteps, sessiondomain.CheckpointStep{ID: fmt.Sprintf("step-%d", step.Index), Summary: nonEmptyStopReason(step.Decision.NextAction, step.Decision.Intent, "completed")})
		}
		for _, observation := range step.Observations {
			statusText := "completed"
			if observation.Error != "" {
				statusText = "failed"
			}
			payload.ToolSummaries = append(payload.ToolSummaries, sessiondomain.CheckpointToolSummary{Name: observation.ToolName, Status: statusText, Summary: foldSummary(nonEmptyStopReason(observation.Result.Text, observation.Error, statusText))})
		}
	}
	for _, evidence := range result.State.Evidence {
		item := sessiondomain.CheckpointEvidence{Kind: string(evidence.Kind), Summary: foldSummary(evidence.Summary), Verified: evidence.Verified}
		if evidence.Artifact != nil && evidence.Artifact.Path != "" {
			item.Paths = []string{evidence.Artifact.Path}
			payload.RelevantPaths = append(payload.RelevantPaths, evidence.Artifact.Path)
		}
		payload.Evidence = append(payload.Evidence, item)
	}
	if status != sessiondomain.RunCompleted {
		payload.PendingWork = []string{"Re-plan the interrupted objective from current workspace state."}
	}
	usage, _ := json.Marshal(result.State.Budget)
	payload.Usage = usage
	reason := sessiondomain.CheckpointRunFailed
	if status == sessiondomain.RunCompleted {
		reason = sessiondomain.CheckpointRunCompleted
	} else if status == sessiondomain.RunCancelled {
		reason = sessiondomain.CheckpointUserCancelled
	}
	return sessiondomain.NewCheckpoint(id, runID, sequence, reason, payload, at)
}

func configuredAgentBudget(agent config.AgentConfig) engine.Budget {
	return engine.Budget{
		MaxSteps:        agent.MaxSteps,
		MaxToolCalls:    agent.MaxToolCalls,
		MaxInputTokens:  agent.MaxInputTokens,
		MaxOutputTokens: agent.MaxOutputTokens,
		MaxDuration:     agent.MaxDuration,
	}
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
