package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/react"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	openaiadapter "github.com/Godric-W/Amadeus/internal/llm/openai"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/snapshot"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/tool/builtin"
	"github.com/Godric-W/Amadeus/internal/web"
	"github.com/Godric-W/Amadeus/prompts"
)

type ClientFactory func(string, config.ProviderConfig) (llm.Client, error)

type SnapshotFactory func(project.Root) (snapshot.Service, error)

type AgentOptions struct {
	ClientFactory    ClientFactory
	SnapshotFactory  SnapshotFactory
	SnapshotRunID    string
	PostWriteHooks   []react.PostExecutionHook
	UserSkillRoot    string
	UserMCPRoot      string
	MCPClientFactory mcp.ClientFactory
	WebFetcher       web.Fetcher
	WebSearch        web.SearchProvider
}

type Agent struct {
	ProviderName     string
	Project          project.Root
	Client           llm.Client
	Events           event.Sink
	Audit            audit.Sink
	ContextBuilder   *agentcontext.Builder
	PromptRepository *internalprompt.Repository
	PromptAssembler  *internalprompt.Assembler
	AgentPrompt      internalprompt.Bundle
	Registry         *tool.Registry
	Validator        *tool.ArgumentValidator
	Grants           *policy.GrantCache
	Authorizer       *policy.ToolAuthorizer
	ToolExecutor     *react.ToolExecutor
	Iterator         *react.Iterator
	Progress         *react.ProgressMonitor
	Runner           *react.Runner
	PlanController   plan.PlanController
	Planner          plan.DraftPlanner
	Replanner        plan.FixedReplanner
	Snapshots        snapshot.Service
	SnapshotRun      *snapshot.RunTracker
	Skills           *skill.Catalog
	SkillWarnings    []error
	MCP              *mcp.Manager
	MCPWarnings      []error
	WebFetcher       web.Fetcher
	WebSearch        web.SearchProvider
	tools            []tool.Spec
}

func NewAgent(configured config.Config, root project.Root, events event.Sink, approvals policy.ApprovalHandler, auditSink audit.Sink) (*Agent, error) {
	return NewAgentWithOptions(configured, root, events, approvals, auditSink, AgentOptions{})
}

func NewAgentWithOptions(configured config.Config, root project.Root, events event.Sink, approvals policy.ApprovalHandler, auditSink audit.Sink, options AgentOptions) (*Agent, error) {
	createClient := options.ClientFactory
	if createClient == nil {
		createClient = defaultClientFactory
	}
	createSnapshots := options.SnapshotFactory
	if createSnapshots == nil {
		createSnapshots = defaultSnapshotFactory
	}
	return newAgentWithSnapshotFactory(configured, root, events, approvals, auditSink, createClient, createSnapshots, options.SnapshotRunID, options.PostWriteHooks, options.UserSkillRoot, options.UserMCPRoot, options.MCPClientFactory, options.WebFetcher, options.WebSearch)
}

func (agent *Agent) AvailableTools() []tool.Spec {
	if agent == nil || agent.Registry == nil {
		return nil
	}
	entries := agent.Registry.Snapshot()
	tools := make([]tool.Spec, 0, len(entries))
	for _, entry := range entries {
		tools = append(tools, entry.Spec.Clone())
	}
	return tools
}

func newAgent(configured config.Config, root project.Root, events event.Sink, approvals policy.ApprovalHandler, auditSink audit.Sink, createClient ClientFactory) (*Agent, error) {
	return newAgentWithSnapshotFactory(configured, root, events, approvals, auditSink, createClient, defaultSnapshotFactory, "", nil, "", "", nil, nil, nil)
}

func newAgentWithSnapshotFactory(configured config.Config, root project.Root, events event.Sink, approvals policy.ApprovalHandler, auditSink audit.Sink, createClient ClientFactory, createSnapshots SnapshotFactory, snapshotRunID string, postWriteHooks []react.PostExecutionHook, userSkillRoot, userMCPRoot string, mcpClientFactory mcp.ClientFactory, webFetcher web.Fetcher, webSearch web.SearchProvider) (*Agent, error) {
	if err := config.Validate(configured); err != nil {
		return nil, fmt.Errorf("validate Agent configuration: %w", err)
	}
	if root.Path() == "" {
		return nil, errors.New("bootstrap Agent project root is empty")
	}
	if events == nil {
		return nil, errors.New("bootstrap Agent event sink is nil")
	}
	if approvals == nil {
		return nil, errors.New("bootstrap Agent approval handler is nil")
	}
	if auditSink == nil {
		return nil, errors.New("bootstrap Agent audit sink is nil")
	}
	if createClient == nil {
		return nil, errors.New("bootstrap Agent client factory is nil")
	}
	if createSnapshots == nil {
		return nil, errors.New("bootstrap Agent snapshot factory is nil")
	}

	providerName := configured.DefaultProvider
	provider := configured.Providers[providerName]
	client, err := createClient(providerName, provider)
	if err != nil {
		return nil, fmt.Errorf("create Provider client: %w", err)
	}
	if client == nil {
		return nil, errors.New("create Provider client: factory returned nil")
	}
	promptRepository, err := internalprompt.NewBuiltinRepository()
	if err != nil {
		return nil, fmt.Errorf("create Prompt repository: %w", err)
	}
	promptAssembler, err := internalprompt.NewAssembler(promptRepository)
	if err != nil {
		return nil, fmt.Errorf("create Prompt assembler: %w", err)
	}
	agentPrompt, err := assemblePrompts(promptAssembler, prompts.AgentLayers())
	if err != nil {
		return nil, fmt.Errorf("assemble Agent Prompt: %w", err)
	}
	plannerPrompt, err := assemblePrompts(promptAssembler, []prompts.ID{prompts.Planner})
	if err != nil {
		return nil, fmt.Errorf("assemble planner Prompt: %w", err)
	}
	replannerPrompt, err := assemblePrompts(promptAssembler, []prompts.ID{prompts.Replanner})
	if err != nil {
		return nil, fmt.Errorf("assemble replanner Prompt: %w", err)
	}

	registry, err := builtin.NewMVPRegistry(root, builtin.DefaultMVPOptions())
	if err != nil {
		return nil, fmt.Errorf("create MVP tool registry: %w", err)
	}
	snapshots, err := createSnapshots(root)
	if err != nil {
		return nil, fmt.Errorf("create snapshot service: %w", err)
	}
	var snapshotRun *snapshot.RunTracker
	if strings.TrimSpace(snapshotRunID) != "" {
		snapshotRun, err = snapshot.NewRunTracker(snapshots, snapshotRunID)
		if err != nil {
			return nil, fmt.Errorf("create snapshot run tracker: %w", err)
		}
	}
	revertTurn, err := builtin.NewRevertTurn(snapshots)
	if err != nil {
		return nil, fmt.Errorf("create revert_turn tool: %w", err)
	}
	if err := registry.Register(revertTurn); err != nil {
		return nil, fmt.Errorf("register revert_turn tool: %w", err)
	}
	if webFetcher == nil {
		webFetcher, err = web.NewHTTPFetcher(web.PolicyOptions{})
		if err != nil {
			return nil, fmt.Errorf("create web fetcher: %w", err)
		}
	}
	if webSearch == nil {
		webSearch, err = web.NewDuckDuckGoProvider(webFetcher)
		if err != nil {
			return nil, fmt.Errorf("create web search provider: %w", err)
		}
	}
	webFetchTool, err := builtin.NewWebFetch(webFetcher)
	if err != nil {
		return nil, fmt.Errorf("create web_fetch tool: %w", err)
	}
	if err := registry.Register(webFetchTool); err != nil {
		return nil, fmt.Errorf("register web_fetch tool: %w", err)
	}
	webSearchTool, err := builtin.NewWebSearch(webSearch)
	if err != nil {
		return nil, fmt.Errorf("create web_search tool: %w", err)
	}
	if err := registry.Register(webSearchTool); err != nil {
		return nil, fmt.Errorf("register web_search tool: %w", err)
	}
	skills, skillWarnings, err := skill.Load(userSkillRoot, root, skill.DefaultLoadOptions())
	if err != nil {
		return nil, fmt.Errorf("load Skills: %w", err)
	}
	var skillBuffer *skill.ContextBuffer
	if skills.Len() > 0 {
		skillBuffer, err = skill.NewContextBuffer(skill.BufferOptions{})
		if err != nil {
			return nil, fmt.Errorf("create Skill context buffer: %w", err)
		}
		loadSkill, err := builtin.NewLoadSkill(skills, skillBuffer)
		if err != nil {
			return nil, fmt.Errorf("create load_skill tool: %w", err)
		}
		if err := registry.Register(loadSkill); err != nil {
			return nil, fmt.Errorf("register load_skill tool: %w", err)
		}
		readReference, err := builtin.NewReadSkillReference(skills, builtin.ReadSkillReferenceOptions{})
		if err != nil {
			return nil, fmt.Errorf("create read_skill_reference tool: %w", err)
		}
		if err := registry.Register(readReference); err != nil {
			return nil, fmt.Errorf("register read_skill_reference tool: %w", err)
		}
	}
	mcpConfig, err := mcp.Load(userMCPRoot, root, mcp.LoadOptions{})
	if err != nil {
		return nil, fmt.Errorf("load MCP config: %w", err)
	}
	mcpManager, err := mcp.NewManager(mcpConfig, mcpClientFactory)
	if err != nil {
		return nil, fmt.Errorf("create MCP manager: %w", err)
	}
	mcpListTool, mcpCallTool, err := mcp.NewLazyTools(mcpManager)
	if err != nil {
		return nil, fmt.Errorf("create lazy MCP tools: %w", err)
	}
	if mcpListTool != nil {
		if err := registry.Register(mcpListTool); err != nil {
			return nil, fmt.Errorf("register lazy MCP tool: %w", err)
		}
	}
	if mcpCallTool != nil {
		if err := registry.Register(mcpCallTool); err != nil {
			return nil, fmt.Errorf("register lazy MCP tool: %w", err)
		}
	}
	validator := tool.NewArgumentValidator()
	grants := policy.NewGrantCache()
	authorizer, err := policy.NewToolAuthorizerWithOptions(root, approvals, policy.ToolAuthorizerOptions{Grants: grants, Audit: auditSink, Events: events})
	if err != nil {
		return nil, fmt.Errorf("create tool authorizer: %w", err)
	}
	preHooks := make([]react.PreExecutionHook, 0, 1)
	if snapshotRun != nil {
		preHooks = append(preHooks, snapshotRun)
	}
	toolExecutor, err := react.NewToolExecutorWithOptions(registry, validator, react.ToolExecutorOptions{Authorizer: authorizer, Events: events, PreHooks: preHooks, Hooks: postWriteHooks})
	if err != nil {
		return nil, fmt.Errorf("create tool executor: %w", err)
	}
	iterator, err := react.NewIteratorWithOptions(client, newTaskIterationEventSink(events), react.IteratorOptions{SystemPrompt: agentPrompt.Content})
	if err != nil {
		return nil, fmt.Errorf("create model iterator: %w", err)
	}
	progress := react.DefaultProgressMonitor()
	runner, err := react.NewRunner(iterator, toolExecutor, progress, react.RunnerOptions{
		Temperature:        provider.Temperature,
		MaxOutputTokens:    provider.MaxOutputTokens,
		MaxParallelTools:   configured.Agent.MaxParallelTools,
		AdditionalMessages: skillContextMessages(skillBuffer),
		ContextWindow:      agentcontext.NewContextWindowManager(nil),
		ContextProfile:     agentcontext.DefaultContextProfile(provider.ContextWindow, provider.MaxOutputTokens),
		Events:             events,
	})
	if err != nil {
		return nil, fmt.Errorf("create ReAct runner: %w", err)
	}
	reactTaskExecutor, err := plan.NewReActTaskExecutor(runner)
	if err != nil {
		return nil, fmt.Errorf("create default ReAct task executor: %w", err)
	}
	planner, err := plan.NewLLMPlanDraftPlanner(client, plan.PlannerOptions{SystemPrompt: plannerPrompt.Content, MaxAttempts: 2, Temperature: 0, MaxOutputTokens: provider.MaxOutputTokens, Events: events})
	if err != nil {
		return nil, fmt.Errorf("create Planner: %w", err)
	}
	replanner, err := plan.NewLLMReplanner(client, plan.PlannerOptions{SystemPrompt: replannerPrompt.Content, MaxAttempts: 2, Temperature: 0, MaxOutputTokens: provider.MaxOutputTokens, Events: events})
	if err != nil {
		return nil, fmt.Errorf("create Replanner: %w", err)
	}
	planController, err := plan.NewController(planner, reactTaskExecutor, replanner, plan.ControllerOptions{MaxPlanCycles: 8, Events: events})
	if err != nil {
		return nil, fmt.Errorf("create Controller: %w", err)
	}

	entries := registry.Snapshot()
	availableTools := make([]tool.Spec, 0, len(entries))
	for _, entry := range entries {
		availableTools = append(availableTools, entry.Spec.Clone())
	}

	return &Agent{
		ProviderName:     providerName,
		Project:          root,
		Client:           client,
		Events:           events,
		Audit:            auditSink,
		ContextBuilder:   agentcontext.NewBuilder(),
		PromptRepository: promptRepository,
		PromptAssembler:  promptAssembler,
		AgentPrompt:      agentPrompt,
		Registry:         registry,
		Validator:        validator,
		Grants:           grants,
		Authorizer:       authorizer,
		ToolExecutor:     toolExecutor,
		Iterator:         iterator,
		Progress:         progress,
		Runner:           runner,
		PlanController:   planController,
		Planner:          planner,
		Replanner:        replanner,
		Snapshots:        snapshots,
		SnapshotRun:      snapshotRun,
		Skills:           skills,
		SkillWarnings:    append([]error(nil), skillWarnings...),
		MCP:              mcpManager,
		WebFetcher:       webFetcher,
		WebSearch:        webSearch,
		tools:            availableTools,
	}, nil
}

func (agent *Agent) SkillIndex() []skill.IndexEntry {
	if agent == nil || agent.Skills == nil {
		return nil
	}
	return agent.Skills.Index()
}

func skillContextMessages(buffer *skill.ContextBuffer) func() ([]llm.Message, error) {
	if buffer == nil {
		return nil
	}
	return func() ([]llm.Message, error) {
		content, err := skill.MarshalContext(buffer.Consume())
		if err != nil || content == "" {
			return nil, err
		}
		return []llm.Message{llm.DeveloperMessage(content)}, nil
	}
}

func (agent *Agent) RefreshMCP(ctx context.Context) []error {
	if agent == nil || agent.MCP == nil || agent.Registry == nil {
		return nil
	}
	warnings := make([]error, 0)
	for _, server := range agent.MCP.EnabledServers() {
		tools, err := agent.MCP.ToolAdapters(ctx, server, mcp.AdapterOptions{})
		if err != nil {
			warnings = append(warnings, fmt.Errorf("refresh MCP server %q: %w", server, err))
			continue
		}
		values := make([]tool.Tool, len(tools))
		for index, value := range tools {
			values[index] = value
		}
		if err := agent.Registry.ReplaceGroup("mcp:"+server, values); err != nil {
			warnings = append(warnings, fmt.Errorf("register MCP server %q tools: %w", server, err))
		}
	}
	agent.MCPWarnings = append([]error(nil), warnings...)
	return warnings
}

func (agent *Agent) Close() error {
	if agent == nil || agent.MCP == nil {
		return nil
	}
	return agent.MCP.Close()
}

func defaultSnapshotFactory(root project.Root) (snapshot.Service, error) {
	return snapshot.NewFileService(root, snapshot.FileServiceOptions{})
}

func assemblePrompts(assembler *internalprompt.Assembler, ids []prompts.ID) (internalprompt.Bundle, error) {
	layers := make([]string, len(ids))
	for index, id := range ids {
		layers[index] = string(id)
	}
	return assembler.Assemble(internalprompt.AssembleInput{Layers: layers})
}

func defaultClientFactory(providerName string, provider config.ProviderConfig) (llm.Client, error) {
	return openaiadapter.NewAdapter(providerName, provider)
}
