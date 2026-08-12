package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/react"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	openaiadapter "github.com/Godric-W/Amadeus/internal/llm/openai"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/policy"
	processdomain "github.com/Godric-W/Amadeus/internal/process"
	"github.com/Godric-W/Amadeus/internal/project"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/tool/builtin"
	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type ClientFactory func(string, config.ProviderConfig) (llm.Client, error)

type AgentOptions struct {
	ClientFactory      ClientFactory
	PatchProjector     builtin.PatchProjector
	RolloutRecorder    react.RolloutRecorder
	PlanState          *plan.State
	PlanRecorder       builtin.PlanUpdateRecorder
	UserSkillRoot      string
	UserMCPRoot        string
	MCPClientFactory   mcp.ClientFactory
	Skills             *skill.Catalog
	SkillWarnings      []error
	MCP                *mcp.Manager
	WebFetcher         webfetch.Fetcher
	WebSearch          websearch.Provider
	FileSystemPolicy   *project.FileSystemPolicy
	RunPermissions     *project.PermissionStore
	SessionPermissions *project.PermissionStore
	SessionApprovals   *policy.SessionApprovalStore
	FileApprovals      *policy.FileApprovalStore
	ExternalApprovals  *policy.SessionRuleStore
}

type Agent struct {
	ProviderName     string
	Project          project.Root
	Client           llm.Client
	Events           event.Sink
	Audit            audit.Sink
	BaseInstructions llm.BaseInstructions
	Registry         *tool.Registry
	ToolRouter       *tool.Router
	Iterator         *react.Iterator
	Progress         *react.ProgressMonitor
	Runner           *react.Runner
	Skills           *skill.Catalog
	SkillWarnings    []error
	MCP              *mcp.Manager
	MCPWarnings      []error
	WebFetcher       webfetch.Fetcher
	WebSearch        websearch.Provider
	Processes        *processdomain.Manager
	ownMCP           bool
	tools            []tool.Spec
	visibility       map[string]bool
}

func NewAgent(configured config.Config, root project.Root, events event.Sink, approvals policy.ApprovalHandler, auditSink audit.Sink) (*Agent, error) {
	return NewAgentWithOptions(configured, root, events, approvals, auditSink, AgentOptions{})
}

func NewAgentWithOptions(configured config.Config, root project.Root, events event.Sink, approvals policy.ApprovalHandler, auditSink audit.Sink, options AgentOptions) (*Agent, error) {
	createClient := options.ClientFactory
	if createClient == nil {
		createClient = defaultClientFactory
	}
	return newAgentWithOptions(configured, root, events, approvals, auditSink, createClient, options.PatchProjector, options.RolloutRecorder, options.PlanState, options.PlanRecorder, options.UserSkillRoot, options.UserMCPRoot, options.MCPClientFactory, options.Skills, options.SkillWarnings, options.MCP, options.WebFetcher, options.WebSearch, options.FileSystemPolicy, options.RunPermissions, options.SessionPermissions, options.SessionApprovals, options.FileApprovals, options.ExternalApprovals)
}

func (agent *Agent) AvailableTools() []tool.Spec {
	if agent == nil || agent.Registry == nil {
		return nil
	}
	entries := agent.Registry.VisibleSnapshot(agent.visibility)
	tools := make([]tool.Spec, 0, len(entries))
	for _, entry := range entries {
		if legacyToolName(entry.Spec.Name) {
			continue
		}
		tools = append(tools, entry.Spec.Clone())
	}
	return tools
}

func legacyToolName(name string) bool {
	switch name {
	case "apply_patch", "read_file", "list_dir", "glob_files", "grep_code", "request_permissions":
		return true
	default:
		return false
	}
}

func newAgent(configured config.Config, root project.Root, events event.Sink, approvals policy.ApprovalHandler, auditSink audit.Sink, createClient ClientFactory) (*Agent, error) {
	return newAgentWithOptions(configured, root, events, approvals, auditSink, createClient, nil, nil, nil, nil, "", "", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func newAgentWithOptions(configured config.Config, root project.Root, events event.Sink, approvals policy.ApprovalHandler, auditSink audit.Sink, createClient ClientFactory, patchProjector builtin.PatchProjector, rolloutRecorder react.RolloutRecorder, planState *plan.State, planRecorder builtin.PlanUpdateRecorder, userSkillRoot, userMCPRoot string, mcpClientFactory mcp.ClientFactory, externalSkills *skill.Catalog, externalSkillWarnings []error, externalMCP *mcp.Manager, webFetcher webfetch.Fetcher, webSearch websearch.Provider, fileSystemPolicy *project.FileSystemPolicy, runPermissions, sessionPermissions *project.PermissionStore, sessionApprovals *policy.SessionApprovalStore, fileApprovals *policy.FileApprovalStore, externalApprovals *policy.SessionRuleStore) (*Agent, error) {
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
	if externalApprovals == nil {
		externalApprovals = policy.NewSessionRuleStore()
	}
	if createClient == nil {
		return nil, errors.New("bootstrap Agent client factory is nil")
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
	assets, err := internalprompt.LoadAssets()
	if err != nil {
		return nil, fmt.Errorf("load prompt assets: %w", err)
	}
	mvpOptions := builtin.DefaultMVPOptions()
	if fileSystemPolicy == nil {
		fileSystemPolicy, err = project.NewFileSystemPolicy(project.FileSystemPolicyOptions{CWD: root.Path(), Profile: project.PermissionProfile{WorkspaceRoots: []string{root.Path()}}})
		if err != nil {
			return nil, fmt.Errorf("create default filesystem policy: %w", err)
		}
	}
	mvpOptions.FileSystemPolicy = fileSystemPolicy
	mvpOptions.ApplyPatch.Executor.FileSystemPolicy = fileSystemPolicy
	mvpOptions.ApplyPatch.Projector = patchProjector
	mvpOptions.ExecuteCommand.FileSystemPolicy = fileSystemPolicy
	mvpOptions.ExecuteCommand.Approvals = approvals
	mvpOptions.ExecuteCommand.SessionApprovals = sessionApprovals
	mvpOptions.ExecuteCommand.Events = events
	mvpOptions.ExecuteCommand.Audit = auditSink
	registry, err := builtin.NewMVPRegistry(root, mvpOptions)
	if err != nil {
		return nil, fmt.Errorf("create MVP tool registry: %w", err)
	}
	fileTools, err := builtin.NewFileTools(root, builtin.FileToolsOptions{
		FileSystemPolicy: fileSystemPolicy, Approvals: approvals, SessionApprovals: fileApprovals,
		MaxBytes: mvpOptions.ReadFile.MaxBytes, MaxLineBytes: mvpOptions.ReadFile.MaxLineBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("create file tools: %w", err)
	}
	for _, candidate := range []tool.Handler{fileTools.ReadTool(), fileTools.EditTool(), fileTools.WriteTool()} {
		if err := registry.Register(candidate); err != nil {
			return nil, fmt.Errorf("register file tool %q: %w", candidate.Spec().Name, err)
		}
	}
	if legacyGlob, ok := registry.Lookup("glob_files"); ok {
		if err := registry.Register(builtin.NewGlobAlias(legacyGlob)); err != nil {
			return nil, fmt.Errorf("register glob tool: %w", err)
		}
	}
	if legacyGrep, ok := registry.Lookup("grep_code"); ok {
		if err := registry.Register(builtin.NewGrepAlias(legacyGrep)); err != nil {
			return nil, fmt.Errorf("register grep tool: %w", err)
		}
	}
	if planState != nil || planRecorder != nil {
		updatePlan, planErr := builtin.NewUpdatePlan(planState, builtin.UpdatePlanOptions{Events: events, Recorder: planRecorder})
		if planErr != nil {
			return nil, fmt.Errorf("create update_plan tool: %w", planErr)
		}
		if planErr := registry.Register(updatePlan); planErr != nil {
			return nil, fmt.Errorf("register update_plan tool: %w", planErr)
		}
	}
	executeTool, exists := registry.Lookup("execute_command")
	if !exists {
		return nil, errors.New("create MVP tool registry: execute_command is missing")
	}
	executeCommand, ok := executeTool.(*builtin.ExecuteCommand)
	if !ok {
		return nil, errors.New("create MVP tool registry: execute_command has unexpected type")
	}
	visibility := make(map[string]bool)
	if client.Capabilities().SupportsImages {
		viewImage, imageErr := builtin.NewViewImage(root, builtin.ViewImageOptions{FileSystemPolicy: fileSystemPolicy})
		if imageErr != nil {
			return nil, fmt.Errorf("create view_image tool: %w", imageErr)
		}
		if imageErr := registry.RegisterWithRegistration(viewImage, tool.Registration{Exposure: tool.ExposureConditional, Condition: "provider.images"}); imageErr != nil {
			return nil, fmt.Errorf("register view_image tool: %w", imageErr)
		}
		visibility["provider.images"] = true
	}
	if configured.Web.Fetch.Enabled {
		if webFetcher == nil {
			webFetcher, err = webfetch.New(webfetch.Options{MaxBytes: configured.Web.Fetch.MaxBytes, MaxRedirects: configured.Web.Fetch.MaxRedirects, Timeout: configured.Web.Fetch.Timeout})
			if err != nil {
				return nil, fmt.Errorf("create web fetcher: %w", err)
			}
		}
		webFetchTool, toolErr := builtin.NewWebFetchWithApproval(webFetcher, builtin.WebApprovalOptions{Approvals: approvals, Rules: externalApprovals, Events: events})
		if toolErr != nil {
			return nil, fmt.Errorf("create web_fetch tool: %w", toolErr)
		}
		if toolErr := registry.RegisterWithRegistration(webFetchTool, tool.Registration{Exposure: tool.ExposureConditional, Condition: "web.fetch.configured"}); toolErr != nil {
			return nil, fmt.Errorf("register web_fetch tool: %w", toolErr)
		}
		visibility["web.fetch.configured"] = true
	}
	if configured.Web.Search.Enabled {
		if webSearch == nil {
			provider, providerErr := websearch.NewProvider(websearch.ProviderOptions{Name: string(configured.Web.Search.Provider), APIKey: configured.Web.Search.APIKey, BaseURL: configured.Web.Search.BaseURL})
			if providerErr != nil {
				return nil, fmt.Errorf("create web search provider: %w", providerErr)
			}
			webSearch, providerErr = websearch.NewService(provider, websearch.ServiceOptions{Timeout: configured.Web.Search.Timeout, MaxResults: configured.Web.Search.MaxResults})
			if providerErr != nil {
				return nil, fmt.Errorf("create web search service: %w", providerErr)
			}
		}
		webSearchTool, toolErr := builtin.NewWebSearch(webSearch)
		if toolErr != nil {
			return nil, fmt.Errorf("create web_search tool: %w", toolErr)
		}
		if toolErr := registry.RegisterWithRegistration(webSearchTool, tool.Registration{Exposure: tool.ExposureConditional, Condition: "web.search.configured"}); toolErr != nil {
			return nil, fmt.Errorf("register web_search tool: %w", toolErr)
		}
		visibility["web.search.configured"] = true
	}
	skills := externalSkills
	skillWarnings := append([]error(nil), externalSkillWarnings...)
	if skills == nil {
		skills, skillWarnings, err = skill.Load(userSkillRoot, root, skill.DefaultLoadOptions())
		if err != nil {
			return nil, fmt.Errorf("load Skills: %w", err)
		}
	}
	if skills.Len() > 0 {
		readSkill, err := builtin.NewReadSkill(skills, builtin.ReadSkillOptions{})
		if err != nil {
			return nil, fmt.Errorf("create read_skill tool: %w", err)
		}
		if err := registry.RegisterWithRegistration(readSkill, tool.Registration{Exposure: tool.ExposureConditional, Condition: "skills.available"}); err != nil {
			return nil, fmt.Errorf("register read_skill tool: %w", err)
		}
		visibility["skills.available"] = true
	}
	mcpManager := externalMCP
	ownMCP := false
	if mcpManager == nil {
		mcpConfig, loadErr := mcp.Load(userMCPRoot, root, mcp.LoadOptions{})
		if loadErr != nil {
			return nil, fmt.Errorf("load MCP config: %w", loadErr)
		}
		mcpManager, err = mcp.NewManager(mcpConfig, mcpClientFactory)
		if err != nil {
			return nil, fmt.Errorf("create MCP manager: %w", err)
		}
		ownMCP = true
	}
	mcpListTool, mcpCallTool, err := mcp.NewLazyToolsWithApproval(mcpManager, mcp.LazyApprovalOptions{Approvals: approvals, Rules: externalApprovals, Events: events})
	if err != nil {
		return nil, fmt.Errorf("create lazy MCP tools: %w", err)
	}
	if mcpListTool != nil {
		if err := registry.RegisterWithRegistration(mcpListTool, tool.Registration{Exposure: tool.ExposureConditional, Condition: "mcp.configured"}); err != nil {
			return nil, fmt.Errorf("register lazy MCP tool: %w", err)
		}
	}
	if mcpCallTool != nil {
		if err := registry.RegisterWithRegistration(mcpCallTool, tool.Registration{Exposure: tool.ExposureDeferred, Condition: "mcp.catalog"}); err != nil {
			return nil, fmt.Errorf("register lazy MCP tool: %w", err)
		}
	}
	mcpListResources, mcpReadResource, err := mcp.NewResourceTools(mcpManager)
	if err != nil {
		return nil, fmt.Errorf("create MCP resource tools: %w", err)
	}
	if mcpListResources != nil {
		if err := registry.RegisterWithRegistration(mcpListResources, tool.Registration{Exposure: tool.ExposureConditional, Condition: "mcp.resources"}); err != nil {
			return nil, fmt.Errorf("register MCP resource list tool: %w", err)
		}
	}
	if mcpReadResource != nil {
		if err := registry.RegisterWithRegistration(mcpReadResource, tool.Registration{Exposure: tool.ExposureDeferred, Condition: "mcp.resources"}); err != nil {
			return nil, fmt.Errorf("register MCP resource read tool: %w", err)
		}
	}
	if len(mcpManager.EnabledServers()) > 0 {
		visibility["mcp.configured"] = true
		visibility["mcp.catalog"] = true
		visibility["mcp.resources"] = true
	}
	toolRouter, err := tool.NewRouter(registry, tool.NewArgumentValidator(), tool.RouterOptions{
		Observer: react.NewToolEventObserver(events), MaxParallel: configured.Agent.MaxParallelTools, Visibility: visibility,
	})
	if err != nil {
		return nil, fmt.Errorf("create tool router: %w", err)
	}
	iterator, err := react.NewIterator(client, events)
	if err != nil {
		return nil, fmt.Errorf("create model iterator: %w", err)
	}
	progress := react.DefaultProgressMonitor()
	runner, err := react.NewRunner(iterator, toolRouter, progress, react.RunnerOptions{
		Temperature:      provider.Temperature,
		MaxParallelTools: configured.Agent.MaxParallelTools,
		Events:           events,
		Rollout:          rolloutRecorder,
	})
	if err != nil {
		return nil, fmt.Errorf("create ReAct runner: %w", err)
	}
	entries := registry.VisibleSnapshot(visibility)
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
		BaseInstructions: assets.Base,
		Registry:         registry,
		ToolRouter:       toolRouter,
		Iterator:         iterator,
		Progress:         progress,
		Runner:           runner,
		Skills:           skills,
		SkillWarnings:    append([]error(nil), skillWarnings...),
		MCP:              mcpManager,
		WebFetcher:       webFetcher,
		WebSearch:        webSearch,
		Processes:        executeCommand.ProcessManager(),
		ownMCP:           ownMCP,
		tools:            availableTools,
		visibility:       visibility,
	}, nil
}

func (agent *Agent) SkillIndex() []skill.IndexEntry {
	if agent == nil || agent.Skills == nil {
		return nil
	}
	return agent.Skills.Index()
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
		values := make([]tool.Handler, len(tools))
		for index, value := range tools {
			values[index] = value
		}
		if err := agent.Registry.ReplaceGroupWithRegistration("mcp:"+server, values, tool.Registration{Exposure: tool.ExposureDeferred, Condition: "mcp.catalog"}); err != nil {
			warnings = append(warnings, fmt.Errorf("register MCP server %q tools: %w", server, err))
		}
	}
	agent.MCPWarnings = append([]error(nil), warnings...)
	return warnings
}

func (agent *Agent) Close() error {
	if agent == nil {
		return nil
	}
	if agent.Processes != nil {
		agent.Processes.Close()
	}
	if agent.MCP == nil || !agent.ownMCP {
		return nil
	}
	return agent.MCP.Close()
}

func defaultClientFactory(providerName string, provider config.ProviderConfig) (llm.Client, error) {
	return openaiadapter.NewAdapter(providerName, provider)
}
