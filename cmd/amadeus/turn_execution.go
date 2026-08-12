package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/task"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	bootstrap "github.com/Godric-W/Amadeus/internal/app/bootstrap"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/instruction"
	interfacecli "github.com/Godric-W/Amadeus/internal/interface/cli"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/render"
	"github.com/Godric-W/Amadeus/internal/rollout"
	threadmanager "github.com/Godric-W/Amadeus/internal/thread/manager"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func (runner *agentController) newTurnContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	factory := runner.runtime.agentContextFactory
	if factory == nil {
		factory = interruptibleTurnContext
	}
	ctx, cancel := factory(parent)
	if ctx == nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		return nil, nil, errors.New("Coding Agent turn context factory returned nil")
	}
	return ctx, cancel, nil
}

func (runner *agentController) runOnce(ctx context.Context, invocation agentInvocation) error {
	objective, err := parseAgentTask(invocation.Task)
	if err != nil {
		return err
	}
	invocation.Task = objective
	manager, configured, err := runner.ensureThreadManager(ctx, invocation)
	if err != nil {
		return err
	}
	runner.threadMutex.Lock()
	active := runner.currentThread
	runner.threadMutex.Unlock()
	if active == nil {
		active, err = manager.StartThread(ctx, threadmanager.StartInput{Configuration: sessionConfiguration(configured, invocation)})
		if err != nil {
			return err
		}
		runner.threadMutex.Lock()
		runner.currentThread = active
		runner.threadMutex.Unlock()
	}
	runner.threadMutex.Lock()
	factory := runner.taskFactories[active.ID()]
	runner.threadMutex.Unlock()
	if factory == nil {
		return errors.New("active thread task factory is unavailable")
	}
	result, err := factory.Prepare(ctx, invocation)
	if err != nil {
		return err
	}
	mode := turn.PermissionModeDefault
	if invocation.RunMode == "plan" {
		mode = turn.PermissionModePlan
	}
	if err := active.Submit(ctx, protocol.ThreadSettingsOp{PermissionMode: string(mode)}); err != nil {
		return err
	}
	if err := active.Submit(ctx, protocol.UserInputOp{Content: objective}); err != nil {
		return err
	}
	return runner.waitTurn(ctx, active, result)
}

func (runner *agentController) waitTurn(ctx context.Context, active *threadmanager.AmadeusThread, taskResult <-chan error) error {
	io := active.Io()
	var turnID rollout.TurnID
	var executionErr error
	resultReceived := false
	interruptSent := false
	for {
		select {
		case err, ok := <-taskResult:
			if ok {
				executionErr = err
			}
			resultReceived = true
		case eventValue, ok := <-io.Events:
			if !ok {
				if executionErr != nil {
					return executionErr
				}
				return errors.New("thread terminated before turn completion")
			}
			switch eventValue.Message.(type) {
			case protocol.TurnStarted:
				turnID = eventValue.TurnID
			case protocol.TurnRejected:
				message := eventValue.Message.(protocol.TurnRejected)
				return errors.New(message.Error)
			case protocol.TurnCompleted:
				if turnID == "" || eventValue.TurnID == turnID {
					message := eventValue.Message.(protocol.TurnCompleted)
					if message.Error != "" {
						executionErr = errors.New(message.Error)
					}
					if !resultReceived {
						select {
						case err, ok := <-taskResult:
							if ok {
								executionErr = err
							}
						default:
						}
					}
					return executionErr
				}
			case protocol.TurnAborted:
				if turnID == "" || eventValue.TurnID == turnID {
					message := eventValue.Message.(protocol.TurnAborted)
					if executionErr == nil && message.Reason != "" {
						executionErr = errors.New(message.Reason)
					}
					return executionErr
				}
			case protocol.StreamError:
				message := eventValue.Message.(protocol.StreamError)
				if executionErr == nil {
					executionErr = errors.New(message.Error)
				}
			}
		case <-ctx.Done():
			if !interruptSent {
				interruptSent = true
				interruptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				_ = active.Submit(interruptCtx, protocol.InterruptOp{})
				cancel()
			}
		}
	}
}

func (runner *agentController) executeCodingTurn(ctx context.Context, factory *codingTaskFactory, invocation agentInvocation, host task.Host, turnContext *turn.Context, _ []task.Input) (result task.Result, runErr error) {
	configured := factory.configured
	workspaceRoots := append([]string{invocation.Project.Path()}, invocation.WorkspaceRoots...)
	deniedRoots := append(project.DefaultDeniedRoots(), amadeusDeniedRoots(runner.runtime.amadeusRoot)...)
	readOnlyRoots := workspaceReadOnlyRoots(workspaceRoots)
	temporaryRoots := project.DefaultTemporaryRoots()
	fileSystemPolicy, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{
		CWD: invocation.Project.Path(),
		Profile: project.PermissionProfile{
			ReadHost: true, WorkspaceRoots: workspaceRoots, TemporaryRoots: temporaryRoots,
			ReadOnlyRoots: readOnlyRoots, DeniedRoots: deniedRoots,
		},
	})
	if err != nil {
		return task.Result{}, err
	}
	ctx = event.WithMetadata(ctx, event.Metadata{SessionID: string(turnContext.ThreadID), TurnID: string(turnContext.TurnID)})
	ctx = tool.WithInvocationMetadata(ctx, tool.InvocationMetadata{SessionID: string(turnContext.ThreadID), TurnID: string(turnContext.TurnID), Source: tool.ToolCallSourceModel})

	renderer, approvals, err := runner.turnInterface(invocation)
	if err != nil {
		return task.Result{}, err
	}
	eventHub, err := event.NewHub(renderer)
	if err != nil {
		return task.Result{}, err
	}
	defer func() { runErr = errors.Join(runErr, eventHub.Close()) }()
	auditFactory := runner.runtime.auditSinkFactory
	if auditFactory == nil {
		auditFactory = defaultAuditSinkFactory(runner.runtime.lookupEnv, os.UserHomeDir)
	}
	auditSink, auditCloser, err := auditFactory()
	if err != nil {
		return task.Result{}, err
	}
	if auditSink == nil {
		return task.Result{}, errors.New("Coding Agent audit factory returned nil sink")
	}
	if auditCloser != nil {
		defer func() { runErr = errors.Join(runErr, auditCloser.Close()) }()
	}
	planState := plan.NewState()
	extensions, err := factory.ensureExtensions()
	if err != nil {
		return task.Result{}, err
	}
	options := bootstrap.AgentOptions{
		UserSkillRoot: runner.runtime.amadeusRoot, UserMCPRoot: runner.runtime.amadeusRoot,
		MCPClientFactory: runner.runtime.mcpClientFactory, WebFetcher: runner.runtime.webFetcher, WebSearch: runner.runtime.webSearch,
		RolloutRecorder: &turnRolloutRecorder{host: host, turnID: turnContext.TurnID},
		PlanState:       planState, FileSystemPolicy: fileSystemPolicy, SessionApprovals: factory.sessionApprovals,
		FileApprovals:     factory.fileApprovals,
		ExternalApprovals: factory.externalApprovals,
		Skills:            extensions.Skills(), SkillWarnings: extensions.SkillWarnings(), MCP: extensions.MCP(),
		PlanRecorder: func(recordCtx context.Context, snapshot plan.Snapshot) error {
			return recordPlanUpdate(recordCtx, host, turnContext.TurnID, snapshot)
		},
	}
	if runner.runtime.llmClientFactory != nil {
		options.ClientFactory = func(providerName string, providerConfig config.ProviderConfig) (llm.Client, error) {
			return runner.runtime.llmClientFactory(providerName, providerConfig)
		}
	}
	agent, err := bootstrap.NewAgentWithOptions(configured, invocation.Project, eventHub, approvals, auditSink, options)
	if err != nil {
		return task.Result{}, err
	}
	defer func() { runErr = errors.Join(runErr, agent.Close()) }()
	if agent.Processes != nil {
		defer agent.Processes.CloseOwner(string(turnContext.TurnID))
	}
	if err := runner.publishAgentDiagnostics(ctx, agent); err != nil {
		return task.Result{}, err
	}
	resolver, err := runner.instructionResolver(invocation)
	if err != nil {
		return task.Result{}, err
	}
	if err := prepareTurnContext(ctx, contextPreparationOptions{
		Task: invocation.Task, TurnContext: turnContext, Host: host, Agent: agent, Extensions: extensions,
		FileSystemPolicy: fileSystemPolicy, Instructions: resolver, ApprovalCount: factory.sessionApprovals.Count,
	}); err != nil {
		return task.Result{}, err
	}
	contextHost, ok := host.(task.ContextHost)
	if !ok || contextHost.Context() == nil {
		return task.Result{}, errors.New("session task host does not expose ContextManager")
	}
	provider := configured.Providers[configured.DefaultProvider]
	modelInfo := agent.Client.Model()
	modelInfo.ContextWindow = provider.ContextWindow
	modelInfo.AutoCompactTokenLimit = provider.AutoCompactTokenLimit
	modelInfo.ToolOutputMaxTokens = provider.ToolOutputMaxTokens
	modelInfo = modelInfo.Normalized()
	availableTools := agent.AvailableTools()
	if turnContext.InitialPermissionMode == turn.PermissionModePlan {
		availableTools = planModeTools(availableTools)
	}
	promptShape := llm.Prompt{
		BaseInstructions: agent.BaseInstructions,
		Tools:            promptToolDefinitions(availableTools),
		OutputSchema:     append(llm.OutputSchema(nil), turnContext.OutputSchema...),
	}
	autoCompact := func(compactCtx context.Context) error {
		if !contextHost.Context().NeedsCompaction(modelInfo, promptShape) {
			return nil
		}
		compactResult, compactErr := runner.executeCompactTurn(compactCtx, factory, host, turnContext)
		if compactErr != nil {
			// A single-turn history has nothing safe to replace yet. Keep the
			// projected prompt and let the normal Tool Result limit protect it.
			if strings.Contains(compactErr.Error(), "no earlier turn") || strings.Contains(compactErr.Error(), "no safely compactable") {
				return nil
			}
			return compactErr
		}
		if len(compactResult.Items) == 0 {
			return nil
		}
		return host.AppendItems(compactCtx, turnContext.TurnID, compactResult.Items...)
	}
	return runner.executeReactorTurn(ctx, invocation, configured, agent, contextHost.Context(), autoCompact, availableTools, llm.OutputSchema(turnContext.OutputSchema), turnContext.TurnID)
}

func promptToolDefinitions(specs []tool.Spec) []llm.ToolDefinition {
	definitions := make([]llm.ToolDefinition, len(specs))
	for index, spec := range specs {
		definitions[index] = llm.ToolDefinition{
			Name: spec.Name, Description: spec.Description,
			InputSchema: append([]byte(nil), spec.InputSchema...),
		}
	}
	return definitions
}

func (runner *agentController) turnInterface(invocation agentInvocation) (event.Sink, policy.ApprovalHandler, error) {
	if invocation.EventSink != nil || invocation.Approvals != nil {
		if invocation.EventSink == nil || invocation.Approvals == nil {
			return nil, nil, errors.New("Coding Agent external TUI requires both event sink and approval handler")
		}
		return invocation.EventSink, invocation.Approvals, nil
	}
	detectTerminal := runner.runtime.terminalDetector
	if detectTerminal == nil {
		detectTerminal = isTerminalInput
	}
	capabilities := tui.DetectTerminalCapabilitiesWithOptions(invocation.Input, invocation.Output, tui.TerminalCapabilityOptions{
		IsTerminal: func(input io.Reader) bool { return detectTerminal(input) }, ForcePlain: invocation.Plain,
	})
	var renderer event.Sink
	var approvals policy.ApprovalHandler
	var err error
	if capabilities.TTY && !capabilities.Plain {
		renderer, err = tui.NewInlineRenderer(invocation.Output, invocation.ErrorOutput)
		if err == nil {
			approvals, err = tui.NewInlineApprovalPrompt(tui.InlineApprovalPromptOptions{Input: invocation.Input, Output: invocation.ErrorOutput, IsTerminal: func(input io.Reader) bool { return detectTerminal(input) }})
		}
	} else {
		renderer, err = render.NewAgentRenderer(invocation.Output, invocation.ErrorOutput)
		if err == nil {
			approvals, err = interfacecli.NewTerminalApprovalHandler(interfacecli.TerminalApprovalOptions{Input: invocation.Input, Output: invocation.ErrorOutput, IsTerminal: func(input io.Reader) bool { return detectTerminal(input) }})
		}
	}
	return renderer, approvals, err
}

func (runner *agentController) publishAgentDiagnostics(ctx context.Context, agent *bootstrap.Agent) error {
	for _, warning := range agent.SkillWarnings {
		if warning != nil {
			if err := agent.Events.Publish(ctx, event.DiagnosticPublished{Severity: "warn", Code: "skill_load_warning", Message: warning.Error()}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (runner *agentController) instructionResolver(invocation agentInvocation) (*instruction.WorkspaceResolver, error) {
	if runner.runtime.rootErr != nil {
		return nil, fmt.Errorf("resolve Amadeus root for user instructions: %w", runner.runtime.rootErr)
	}
	userLoader, err := instruction.NewUserLoader(runner.runtime.amadeusRoot, instruction.UserLoaderOptions{})
	if err != nil {
		return nil, err
	}
	roots := []project.Root{invocation.Project}
	for _, workspaceRoot := range invocation.WorkspaceRoots {
		resolved, err := project.NewRoot(workspaceRoot)
		if err != nil {
			return nil, err
		}
		roots = append(roots, resolved)
	}
	return instruction.NewWorkspaceResolver(userLoader, roots, instruction.ProjectLoaderOptions{})
}

func amadeusDeniedRoots(root string) []string {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	return []string{filepath.Join(root, "config.yaml"), filepath.Join(root, "mcp.yaml"), filepath.Join(root, "skills"), filepath.Join(root, "data")}
}

func workspaceReadOnlyRoots(workspaceRoots []string) []string {
	values := make([]string, 0, len(workspaceRoots)*2)
	for _, root := range workspaceRoots {
		for _, name := range []string{".git", ".amadeus"} {
			candidate := filepath.Join(root, name)
			if _, err := os.Stat(candidate); err == nil {
				values = append(values, candidate)
			}
		}
	}
	return values
}
