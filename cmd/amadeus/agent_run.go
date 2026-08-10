package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/agent/plan"
	agentruntime "github.com/Godric-W/Amadeus/internal/agent/runtime"
	bootstrap "github.com/Godric-W/Amadeus/internal/app/bootstrap"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	rundiff "github.com/Godric-W/Amadeus/internal/diff"
	extensionruntime "github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/instruction"
	interfacecli "github.com/Godric-W/Amadeus/internal/interface/cli"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/render"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/tool/builtin"
	patchtool "github.com/Godric-W/Amadeus/internal/tool/patch"
)

type patchProjectorGroup []builtin.PatchProjector

func (group patchProjectorGroup) ProjectPatch(ctx context.Context, deltas []patchtool.AppliedPatchDelta) error {
	var combined error
	for _, projector := range group {
		if projector != nil {
			combined = errors.Join(combined, projector.ProjectPatch(ctx, clonePatchDeltas(deltas)))
		}
	}
	return combined
}

func clonePatchDeltas(deltas []patchtool.AppliedPatchDelta) []patchtool.AppliedPatchDelta {
	cloned := make([]patchtool.AppliedPatchDelta, len(deltas))
	for index, delta := range deltas {
		cloned[index] = delta
		cloned[index].OldContent = append([]byte(nil), delta.OldContent...)
		cloned[index].NewContent = append([]byte(nil), delta.NewContent...)
	}
	return cloned
}

func (runner *agentController) newRunContext(parent context.Context) (context.Context, context.CancelFunc, error) {
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

func (runner *agentController) runOnce(ctx context.Context, invocation agentInvocation) (runErr error) {
	objective, err := parseAgentTask(invocation.Task)
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
	sessionRuntime, err := runner.ensureSessionRuntime(ctx, invocation)
	if err != nil {
		return err
	}
	provider := configured.Providers[configured.DefaultProvider]
	workspaceRoots := append([]string{invocation.Project.Path()}, invocation.WorkspaceRoots...)
	deniedRoots := append(project.DefaultDeniedRoots(), amadeusDeniedRoots(runner.runtime.amadeusRoot)...)
	readOnlyRoots := workspaceReadOnlyRoots(workspaceRoots)
	temporaryRoots := project.DefaultTemporaryRoots()
	runMode := sessiondomain.RunModeExecute
	if invocation.RunMode != "" {
		runMode = invocation.RunMode
	}
	started, err := sessionRuntime.BeginRun(context.WithoutCancel(ctx), invocation.Task, sessiondomain.RunMetadata{
		Provider: configured.DefaultProvider, Model: provider.Model, APIMode: string(provider.API), Dialect: string(provider.Dialect),
		Mode: runMode,
	})
	if err != nil {
		return err
	}
	extensions, ok := sessionRuntime.Extension().(*extensionruntime.Runtime)
	if !ok || extensions == nil {
		_, finishErr := sessionRuntime.FinishRun(context.WithoutCancel(ctx), started, sessiondomain.RunFailed, "Session extension runtime is unavailable", "", nil)
		return errors.Join(errors.New("Coding Agent Session extension runtime is unavailable"), finishErr)
	}
	runContext, err := agentruntime.NewRunContext(started.Records.Project, started.Records.Session, started.Records.Run, agentruntime.RunContext{
		Provider: configured.DefaultProvider, Model: provider.Model, Mode: runMode, CWD: invocation.Project.Path(),
		FileSystem: agentruntime.FileSystemProfile{
			ReadHost: true, WorkspaceRoots: workspaceRoots, TemporaryRoots: temporaryRoots,
			ReadOnlyRoots: readOnlyRoots, DeniedRoots: deniedRoots,
		},
		ContextProfile: agentcontext.DefaultContextProfile(provider.ContextWindow),
		Budget:         configuredReactorBudget(configured.Agent).Budget,
	})
	if err != nil {
		_, finishErr := sessionRuntime.FinishRun(context.WithoutCancel(ctx), started, sessiondomain.RunFailed, foldSummary(err.Error()), "", nil)
		return errors.Join(err, finishErr)
	}
	runRuntime, err := agentruntime.NewRunRuntime(ctx, runContext, func(finishCtx context.Context, final agentruntime.FinalState) error {
		usageJSON, marshalErr := json.Marshal(final.State)
		if marshalErr != nil {
			return fmt.Errorf("encode RunRuntime state: %w", marshalErr)
		}
		_, finishErr := sessionRuntime.FinishRun(finishCtx, started, final.Status, final.Reason, final.AssistantContent, usageJSON)
		return finishErr
	})
	if err != nil {
		_, finishErr := sessionRuntime.FinishRun(context.WithoutCancel(ctx), started, sessiondomain.RunFailed, foldSummary(err.Error()), "", nil)
		return errors.Join(err, finishErr)
	}
	fileSystemPolicy, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{
		CWD: invocation.Project.Path(),
		Profile: project.PermissionProfile{
			ReadHost: true, WorkspaceRoots: workspaceRoots, TemporaryRoots: temporaryRoots,
			ReadOnlyRoots: readOnlyRoots, DeniedRoots: deniedRoots,
		},
		RunPermissions: runRuntime.State().PermissionStore(), SessionPermissions: sessionRuntime.PermissionStore(),
	})
	if err != nil {
		_ = runRuntime.Finish(context.WithoutCancel(ctx), sessiondomain.RunFailed, foldSummary(err.Error()), "")
		return err
	}
	ctx = event.WithMetadata(runRuntime.Context(), event.Metadata{
		SessionID: string(started.Records.Session.ID),
		RunID:     string(started.Records.Run.ID),
	})
	ctx = tool.WithInvocationMetadata(ctx, tool.InvocationMetadata{
		SessionID: string(started.Records.Session.ID),
		RunID:     string(started.Records.Run.ID),
		Source:    tool.ToolCallSourceModel,
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
		finishErr := runRuntime.Finish(context.WithoutCancel(ctx), status, reason, "")
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
	runDiff, err := rundiff.NewProjector(invocation.Project.Path(), eventHub)
	if err != nil {
		return err
	}
	if err := runRuntime.State().AttachRunDiff(runDiff); err != nil {
		return err
	}
	patchProjectors := append(patchProjectorGroup{runDiff}, runner.runtime.patchProjectors...)

	options := bootstrap.AgentOptions{
		UserSkillRoot:    runner.runtime.amadeusRoot,
		UserMCPRoot:      runner.runtime.amadeusRoot,
		MCPClientFactory: runner.runtime.mcpClientFactory,
		WebFetcher:       runner.runtime.webFetcher,
		WebSearch:        runner.runtime.webSearch,
		PatchProjector:   patchProjectors,
		RolloutRecorder: &sessionRolloutRecorder{
			runtime: sessionRuntime, runID: started.Records.Run.ID,
			nextID: runner.runtimeID, clock: runner.runtimeNow,
		},
		PlanState:        runRuntime.State().Plan(),
		FileSystemPolicy: fileSystemPolicy,
		RunPermissions:   runRuntime.State().PermissionStore(), SessionPermissions: sessionRuntime.PermissionStore(),
		SessionApprovals: sessionRuntime.ApprovalStore(),
	}
	options.Skills = extensions.Skills()
	options.SkillWarnings = extensions.SkillWarnings()
	options.MCP = extensions.MCP()
	options.PlanRecorder = func(ctx context.Context, snapshot plan.Snapshot) error {
		items := make([]sessiondomain.PlanUpdateItem, 0, len(snapshot.Items))
		for _, item := range snapshot.Items {
			items = append(items, sessiondomain.PlanUpdateItem{Step: item.Step, Status: string(item.Status)})
		}
		payload, err := sessiondomain.EncodePayload(sessiondomain.PlanUpdatePayload{
			Explanation: snapshot.Explanation, Items: items, UpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano), Revision: snapshot.Revision,
		})
		if err != nil {
			return err
		}
		_, err = sessionRuntime.Append(context.WithoutCancel(ctx), started.Records.Run.ID, sessiondomain.AppendItem{
			ID: sessiondomain.RolloutItemID(runner.runtimeID("item")), RunID: started.Records.Run.ID,
			Kind: sessiondomain.RolloutPlanUpdate, Payload: payload, CreatedAt: runner.runtimeNow(),
		})
		return err
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
	if err := runRuntime.RegisterCleanup("Agent", agent.Close); err != nil {
		_ = agent.Close()
		return err
	}
	if agent.Processes != nil {
		if err := runRuntime.RegisterCleanup("Process owner", func() error {
			agent.Processes.CloseOwner(string(started.Records.Run.ID))
			return nil
		}); err != nil {
			return err
		}
	}
	if agent.SandboxDiagnostic != "" {
		if publishErr := agent.Events.Publish(ctx, event.DiagnosticPublished{
			Severity: "warn", Code: "sandbox_unavailable",
			Message: fmt.Sprintf("command isolation is %s: %s", agent.SandboxMode, agent.SandboxDiagnostic),
		}); publishErr != nil {
			return fmt.Errorf("publish command sandbox diagnostic: %w", publishErr)
		}
	}
	for _, warning := range agent.SkillWarnings {
		if warning == nil {
			continue
		}
		if publishErr := agent.Events.Publish(ctx, event.DiagnosticPublished{
			Severity: "warn", Code: "skill_load_warning", Message: warning.Error(),
		}); publishErr != nil {
			return fmt.Errorf("publish Skill load diagnostic: %w", publishErr)
		}
	}
	if runner.runtime.rootErr != nil {
		return fmt.Errorf("resolve Amadeus root for user instructions: %w", runner.runtime.rootErr)
	}
	userLoader, err := instruction.NewUserLoader(runner.runtime.amadeusRoot, instruction.UserLoaderOptions{})
	if err != nil {
		return err
	}
	instructionRoots := []project.Root{invocation.Project}
	for _, workspaceRoot := range invocation.WorkspaceRoots {
		resolvedRoot, rootErr := project.NewRoot(workspaceRoot)
		if rootErr != nil {
			return rootErr
		}
		instructionRoots = append(instructionRoots, resolvedRoot)
	}
	resolver, err := instruction.NewWorkspaceResolver(userLoader, instructionRoots, instruction.ProjectLoaderOptions{})
	if err != nil {
		return err
	}
	requestViewProvider := runner.newRequestViewProvider(requestViewOptions{
		Task: invocation.Task, RunMode: runMode,
		ProviderName: configured.DefaultProvider, Model: provider.Model,
		Budget:       agentcontext.DefaultBudget(runContext.ContextProfile.EffectiveInputLimit()),
		WorkspaceCWD: invocation.Project.Path(), RunContext: runContext,
		SessionRuntime: sessionRuntime, Started: started, Agent: agent,
		Extensions: extensions, FileSystemPolicy: fileSystemPolicy, Instructions: resolver,
	})
	var persisted bool
	persisted, err = runner.executeReactorRun(ctx, runRuntime, invocation, configured, agent, requestViewProvider)
	finished = persisted
	return err
}

func amadeusDeniedRoots(root string) []string {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	return []string{
		filepath.Join(root, "config.yaml"),
		filepath.Join(root, "mcp.yaml"),
		filepath.Join(root, "skills"),
		filepath.Join(root, "data"),
	}
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
