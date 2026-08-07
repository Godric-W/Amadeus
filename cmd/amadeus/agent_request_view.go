package main

import (
	"context"

	"github.com/Godric-W/Amadeus/internal/agent/react"
	agentruntime "github.com/Godric-W/Amadeus/internal/agent/runtime"
	bootstrap "github.com/Godric-W/Amadeus/internal/app/bootstrap"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	extensionruntime "github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/instruction"
	"github.com/Godric-W/Amadeus/internal/project"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type requestViewOptions struct {
	Task             string
	ExecutionMode    agentExecutionMode
	RunMode          sessiondomain.RunMode
	ProviderName     string
	Model            string
	Budget           agentcontext.Budget
	WorkspaceCWD     string
	RunContext       *agentruntime.RunContext
	SessionRuntime   *sessiondomain.SessionRuntime
	Started          sessiondomain.StartedRun
	Agent            *bootstrap.Agent
	Extensions       *extensionruntime.Runtime
	FileSystemPolicy *project.FileSystemPolicy
	Instructions     *instruction.WorkspaceResolver
}

func (runner *agentController) newRequestViewProvider(options requestViewOptions) react.RequestViewProvider {
	return react.RequestViewProviderFunc(func(sampleCtx context.Context, input react.RequestViewInput) (agentcontext.RequestView, error) {
		request, resolved, err := options.Instructions.ResolveTarget(sampleCtx, options.RunContext.CWD, instruction.TargetCommandCWD)
		if err != nil {
			return agentcontext.RequestView{}, err
		}
		tools := options.Agent.AvailableTools()
		if options.ExecutionMode == agentExecutionPlanned {
			tools = planModeTools(tools)
		}
		effectivePermissions := options.FileSystemPolicy.EffectiveProfile()
		developerPrompts, err := internalprompt.ComposeDeveloper(options.Agent.PromptAssembler, string(options.RunMode), toolSpecNames(tools), internalprompt.RuntimeFacts{
			CWD:                         options.RunContext.CWD,
			WorkspaceRoots:              effectivePermissions.Base.WorkspaceRoots,
			TemporaryRoots:              effectivePermissions.Base.TemporaryRoots,
			ReadOnlyRoots:               effectivePermissions.Base.ReadOnlyRoots,
			DeniedRoots:                 effectivePermissions.Base.DeniedRoots,
			ReadHost:                    effectivePermissions.Base.ReadHost,
			IsolationMode:               string(options.Agent.SandboxMode),
			RunWritableRoots:            effectivePermissions.Run.WritableRoots,
			SessionWritableRoots:        effectivePermissions.Session.WritableRoots,
			SessionCommandApprovalCount: options.SessionRuntime.ApprovalStore().Count(),
		})
		if err != nil {
			return agentcontext.RequestView{}, err
		}
		skillInjections, err := options.Extensions.ResolveSkillInjections(options.Task)
		if err != nil {
			return agentcontext.RequestView{}, err
		}
		mcpBinding := options.Extensions.MCPBinding()
		frozenRequest, err := agentruntime.NewRequestContext(
			options.RunContext, options.SessionRuntime.History(), tools, resolved,
			agentruntime.WorkspaceSnapshot{CWD: options.WorkspaceCWD},
			options.RunContext.ContextProfile, options.Extensions.MCPRevision(), options.Extensions.SkillRevision(), skillInjections,
		)
		if err != nil {
			return agentcontext.RequestView{}, err
		}
		historyProjection, err := sessiondomain.ProjectMessages(frozenRequest.History.Items)
		if err != nil {
			return agentcontext.RequestView{}, err
		}
		envelope, err := options.Agent.ContextManager.Build(sampleCtx, agentcontext.BuildInput{
			Prompt: options.Agent.AgentPrompt, DeveloperPrompts: developerPrompts,
			InstructionRequest: request, Instructions: frozenRequest.Instructions,
			Conversation: historyProjection.Messages, ConversationSources: historyProjection.SourceSequences,
			Budget: options.Budget, Task: options.Task, TaskInConversation: true, Tools: frozenRequest.Tools,
			SkillIndex: options.Agent.SkillIndex(), SkillInjections: frozenRequest.SkillInjections,
			Revisions: agentcontext.ContextRevisions{
				MCPBinding: frozenRequest.MCPRevision, SkillCatalog: frozenRequest.SkillRevision, ToolExposure: frozenRequest.ToolRevision,
			},
		})
		if err != nil {
			return agentcontext.RequestView{}, err
		}
		if err := runner.persistCompaction(sampleCtx, options.SessionRuntime, options.Started, historyProjection, envelope, options.ProviderName, options.Model); err != nil {
			return agentcontext.RequestView{}, err
		}
		view, err := options.Agent.ContextManager.Prepare(sampleCtx, agentcontext.WindowRequest{
			Base: envelope, Additional: input.Additional, Profile: frozenRequest.Profile,
			PreviousUsage: input.PreviousUsage, LastSentCount: input.LastSentCount,
		})
		if err != nil {
			return agentcontext.RequestView{}, err
		}
		view.MCPBindingRevision = mcpBinding.Revision
		return view, nil
	})
}

func planModeTools(specs []tool.Spec) []tool.Spec {
	allowedNetwork := map[string]struct{}{
		"web_search": {}, "web_fetch": {}, "mcp_list_tools": {},
		"mcp_list_resources": {}, "mcp_read_resource": {},
	}
	result := make([]tool.Spec, 0, len(specs))
	for _, spec := range specs {
		if spec.Name == "update_plan" || spec.Name == "request_permissions" {
			continue
		}
		if spec.SideEffect == tool.SideEffectNone || spec.SideEffect == tool.SideEffectRead {
			result = append(result, spec.Clone())
			continue
		}
		if _, ok := allowedNetwork[spec.Name]; ok {
			result = append(result, spec.Clone())
		}
	}
	return result
}

func toolSpecNames(specs []tool.Spec) []string {
	names := make([]string, len(specs))
	for index, spec := range specs {
		names[index] = spec.Name
	}
	return names
}
