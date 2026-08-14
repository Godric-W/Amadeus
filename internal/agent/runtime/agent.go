package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
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
	ClientFactory    ClientFactory
	RolloutRecorder  react.RolloutRecorder
	PlanUpdater      builtin.PlanUpdater
	UserSkillRoot    string
	UserMCPRoot      string
	MCPClientFactory mcp.ClientFactory
	Skills           *skill.Catalog
	SkillWarnings    []error
	MCP              *mcp.Manager
	WebFetcher       webfetch.Fetcher
	WebSearch        websearch.Provider
	FileSystemPolicy *project.FileSystemPolicy
	Permissions      *policy.SessionPermissionContext
	BaseInstructions llm.BaseInstructions
}

type Agent struct {
	ProviderName     string
	Project          project.Root
	Client           llm.Client
	Events           protocol.EventSink
	Audit            audit.Sink
	BaseInstructions llm.BaseInstructions
	Registry         *tool.Registry
	ToolService      *tool.ToolExecutionService
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
	tools            []tool.ToolSpec
	visibility       map[string]bool
}

func NewAgent(configured config.Config, root project.Root, events protocol.EventSink, approvals policy.ApprovalPort, auditSink audit.Sink) (*Agent, error) {
	return NewAgentWithOptions(configured, root, events, approvals, auditSink, AgentOptions{})
}

func NewAgentWithOptions(configured config.Config, root project.Root, events protocol.EventSink, approvals policy.ApprovalPort, auditSink audit.Sink, options AgentOptions) (*Agent, error) {
	createClient := options.ClientFactory
	if createClient == nil {
		createClient = DefaultClientFactory
	}
	return newAgentWithOptions(configured, root, events, approvals, auditSink, createClient, options.RolloutRecorder, options.PlanUpdater, options.UserSkillRoot, options.UserMCPRoot, options.MCPClientFactory, options.Skills, options.SkillWarnings, options.MCP, options.WebFetcher, options.WebSearch, options.FileSystemPolicy, options.Permissions, options.BaseInstructions)
}

func (agent *Agent) AvailableTools() []tool.ToolSpec {
	if agent == nil || agent.Registry == nil {
		return nil
	}
	entries := agent.Registry.VisibleSnapshot(agent.visibility)
	tools := make([]tool.ToolSpec, 0, len(entries))
	for _, entry := range entries {
		tools = append(tools, entry.Spec.Clone())
	}
	return tools
}

func newAgent(configured config.Config, root project.Root, events protocol.EventSink, approvals policy.ApprovalPort, auditSink audit.Sink, createClient ClientFactory) (*Agent, error) {
	return newAgentWithOptions(configured, root, events, approvals, auditSink, createClient, nil, nil, "", "", nil, nil, nil, nil, nil, nil, nil, nil, llm.BaseInstructions{})
}

func newAgentWithOptions(configured config.Config, root project.Root, events protocol.EventSink, approvals policy.ApprovalPort, auditSink audit.Sink, createClient ClientFactory, rolloutRecorder react.RolloutRecorder, planUpdater builtin.PlanUpdater, userSkillRoot, userMCPRoot string, mcpClientFactory mcp.ClientFactory, externalSkills *skill.Catalog, externalSkillWarnings []error, externalMCP *mcp.Manager, webFetcher webfetch.Fetcher, webSearch websearch.Provider, fileSystemPolicy *project.FileSystemPolicy, permissions *policy.SessionPermissionContext, baseInstructions llm.BaseInstructions) (*Agent, error) {
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
		return nil, errors.New("bootstrap Agent approval port is nil")
	}
	if auditSink == nil {
		return nil, errors.New("bootstrap Agent audit sink is nil")
	}
	if permissions == nil {
		permissions = policy.NewSessionPermissionContext()
	}
	coordinator, err := policy.NewApprovalCoordinator(approvals)
	if err != nil {
		return nil, fmt.Errorf("create approval coordinator: %w", err)
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
	if strings.TrimSpace(baseInstructions.Text) == "" {
		baseInstructions = assets.Base
	}
	coreOptions := builtin.DefaultCoreToolOptions()
	if fileSystemPolicy == nil {
		fileSystemPolicy, err = project.NewFileSystemPolicy(project.FileSystemPolicyOptions{CWD: root.Path(), Profile: project.PermissionProfile{WorkspaceRoots: []string{root.Path()}}})
		if err != nil {
			return nil, fmt.Errorf("create default filesystem policy: %w", err)
		}
	}
	coreOptions.FileSystemPolicy = fileSystemPolicy
	coreOptions.Events = events
	coreOptions.ExecuteCommand.Audit = auditSink
	processManager := processdomain.NewManager()
	coreOptions.ExecuteCommand.ProcessManager = processManager
	coreOptions.PlanUpdater = planUpdater
	registry, err := builtin.NewCoreRegistry(root, coreOptions)
	if err != nil {
		return nil, fmt.Errorf("create core tool registry: %w", err)
	}
	visibility := make(map[string]bool)
	if client.Capabilities().SupportsImages {
		viewImage, imageErr := builtin.NewViewImage(root, builtin.ViewImageOptions{FileSystemPolicy: fileSystemPolicy})
		if imageErr != nil {
			return nil, fmt.Errorf("create view_image tool: %w", imageErr)
		}
		if imageErr := registry.RegisterDefinitionWithRegistration(viewImage, tool.Registration{Exposure: tool.ExposureConditional, Condition: "provider.images"}); imageErr != nil {
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
		webFetchTool, toolErr := builtin.NewWebFetch(webFetcher)
		if toolErr != nil {
			return nil, fmt.Errorf("create web_fetch tool: %w", toolErr)
		}
		if toolErr := registry.RegisterDefinitionWithRegistration(webFetchTool, tool.Registration{Exposure: tool.ExposureConditional, Condition: "web.fetch.configured"}); toolErr != nil {
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
		if toolErr := registry.RegisterDefinitionWithRegistration(webSearchTool, tool.Registration{Exposure: tool.ExposureConditional, Condition: "web.search.configured"}); toolErr != nil {
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
		if err := registry.RegisterDefinitionWithRegistration(readSkill, tool.Registration{Exposure: tool.ExposureConditional, Condition: "skills.available"}); err != nil {
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
	mcpListTool, mcpCallTool, err := mcp.NewLazyTools(mcpManager)
	if err != nil {
		return nil, fmt.Errorf("create lazy MCP tools: %w", err)
	}
	if mcpListTool != nil {
		if err := registry.RegisterDefinitionWithRegistration(mcpListTool, tool.Registration{Exposure: tool.ExposureConditional, Condition: "mcp.configured"}); err != nil {
			return nil, fmt.Errorf("register lazy MCP tool: %w", err)
		}
	}
	if mcpCallTool != nil {
		if err := registry.RegisterDefinitionWithRegistration(mcpCallTool, tool.Registration{Exposure: tool.ExposureDeferred, Condition: "mcp.catalog"}); err != nil {
			return nil, fmt.Errorf("register lazy MCP tool: %w", err)
		}
	}
	mcpListResources, mcpReadResource, err := mcp.NewResourceTools(mcpManager)
	if err != nil {
		return nil, fmt.Errorf("create MCP resource tools: %w", err)
	}
	if mcpListResources != nil {
		if err := registry.RegisterDefinitionWithRegistration(mcpListResources, tool.Registration{Exposure: tool.ExposureConditional, Condition: "mcp.resources"}); err != nil {
			return nil, fmt.Errorf("register MCP resource list tool: %w", err)
		}
	}
	if mcpReadResource != nil {
		if err := registry.RegisterDefinitionWithRegistration(mcpReadResource, tool.Registration{Exposure: tool.ExposureDeferred, Condition: "mcp.resources"}); err != nil {
			return nil, fmt.Errorf("register MCP resource read tool: %w", err)
		}
	}
	if len(mcpManager.EnabledServers()) > 0 {
		visibility["mcp.configured"] = true
		visibility["mcp.catalog"] = true
		visibility["mcp.resources"] = true
	}
	toolService, err := tool.NewToolExecutionService(registry, tool.NewArgumentValidator(), tool.ToolExecutionServiceOptions{
		Observer: react.NewToolEventObserver(events), MaxParallel: configured.Agent.MaxParallelTools, Visibility: visibility, Approvals: coordinator, Permissions: permissions,
	})
	if err != nil {
		return nil, fmt.Errorf("create tool execution service: %w", err)
	}
	iterator, err := react.NewIterator(client, events)
	if err != nil {
		return nil, fmt.Errorf("create model iterator: %w", err)
	}
	progress := react.DefaultProgressMonitor()
	runner, err := react.NewRunner(iterator, toolService, progress, react.RunnerOptions{
		Temperature:      provider.Temperature,
		MaxParallelTools: configured.Agent.MaxParallelTools,
		Events:           events,
		Rollout:          rolloutRecorder,
	})
	if err != nil {
		return nil, fmt.Errorf("create ReAct runner: %w", err)
	}
	entries := registry.VisibleSnapshot(visibility)
	availableTools := make([]tool.ToolSpec, 0, len(entries))
	for _, entry := range entries {
		availableTools = append(availableTools, entry.Spec.Clone())
	}

	return &Agent{
		ProviderName:     providerName,
		Project:          root,
		Client:           client,
		Events:           events,
		Audit:            auditSink,
		BaseInstructions: baseInstructions,
		Registry:         registry,
		ToolService:      toolService,
		Iterator:         iterator,
		Progress:         progress,
		Runner:           runner,
		Skills:           skills,
		SkillWarnings:    append([]error(nil), skillWarnings...),
		MCP:              mcpManager,
		WebFetcher:       webFetcher,
		WebSearch:        webSearch,
		Processes:        processManager,
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
		definitions := make([]tool.ToolDefinition, len(tools))
		for index, value := range tools {
			definitions[index] = value
		}
		if err := agent.Registry.ReplaceDefinitionGroupWithRegistration("mcp:"+server, definitions, tool.Registration{Exposure: tool.ExposureDeferred, Condition: "mcp.catalog"}); err != nil {
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

func DefaultClientFactory(providerName string, provider config.ProviderConfig) (llm.Client, error) {
	return openaiadapter.NewAdapter(providerName, provider)
}
