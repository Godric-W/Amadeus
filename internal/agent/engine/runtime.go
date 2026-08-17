package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	extensionruntime "github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/instruction"
	"github.com/Godric-W/Amadeus/internal/llm"
	openaiadapter "github.com/Godric-W/Amadeus/internal/llm/openai"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/policy"
	processdomain "github.com/Godric-W/Amadeus/internal/process"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/tool/builtin"
	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type ClientFactory func(string, config.ProviderConfig) (llm.Client, error)

type ServicesOptions struct {
	Config           config.Config
	Project          project.Root
	ClientFactory    ClientFactory
	Events           protocol.EventSink
	Approvals        policy.ApprovalPort
	PlanUpdater      builtin.PlanUpdater
	Audit            audit.Sink
	AuditCloser      io.Closer
	Extensions       *extensionruntime.Runtime
	WebFetcher       webfetch.Fetcher
	WebSearch        websearch.Provider
	FileSystemPolicy *project.FileSystemPolicy
	Permissions      *policy.SessionPermissionContext
	Instructions     *instruction.WorkspaceResolver
	ModelMessages    llm.ModelMessages
}

// Services owns the capabilities shared by every Turn in one Session.
// Turn-specific state is captured by StepContext and never stored here.
type Services struct {
	configured       config.Config
	project          project.Root
	providerName     string
	provider         config.ProviderConfig
	client           llm.Client
	modelMessages    llm.ModelMessages
	registry         *tool.Registry
	toolService      *tool.ToolExecutionService
	processes        *processdomain.Manager
	extensions       *extensionruntime.Runtime
	fileSystemPolicy *project.FileSystemPolicy
	permissions      *policy.SessionPermissionContext
	instructions     *instruction.WorkspaceResolver
	visibility       map[string]bool
	skillWarnings    []error
	budget           TurnBudget
	auditCloser      io.Closer
	closeOnce        sync.Once
	closeErr         error
}

func NewServices(options ServicesOptions) (*Services, error) {
	if err := config.Validate(options.Config); err != nil {
		return nil, fmt.Errorf("validate services configuration: %w", err)
	}
	if options.Project.Path() == "" || options.Events == nil || options.Approvals == nil || options.PlanUpdater == nil || options.Audit == nil {
		return nil, errors.New("services composition is incomplete")
	}
	if options.Extensions == nil || options.FileSystemPolicy == nil || options.Permissions == nil || options.Instructions == nil {
		return nil, errors.New("session services are incomplete")
	}
	if !options.ModelMessages.HasInstructions() {
		return nil, errors.New("services model messages are empty")
	}
	createClient := options.ClientFactory
	if createClient == nil {
		createClient = DefaultClientFactory
	}
	providerName := options.Config.DefaultProvider
	provider := options.Config.Providers[providerName]
	client, err := createClient(providerName, provider)
	if err != nil {
		return nil, fmt.Errorf("create provider client: %w", err)
	}
	if client == nil {
		return nil, errors.New("create provider client: factory returned nil")
	}
	coordinator, err := policy.NewApprovalCoordinator(options.Approvals)
	if err != nil {
		return nil, fmt.Errorf("create approval coordinator: %w", err)
	}
	registry, processes, visibility, err := buildToolRuntime(toolRuntimeOptions{
		configured: options.Config, project: options.Project, client: client, events: options.Events,
		planUpdater: options.PlanUpdater, audit: options.Audit, extensions: options.Extensions,
		webFetcher: options.WebFetcher, webSearch: options.WebSearch, fileSystemPolicy: options.FileSystemPolicy,
	})
	if err != nil {
		return nil, err
	}
	toolService, err := tool.NewToolExecutionService(registry, tool.NewArgumentValidator(), tool.ToolExecutionServiceOptions{
		MaxParallel: options.Config.Agent.MaxParallelTools, Visibility: visibility,
		Approvals: coordinator, Permissions: options.Permissions,
	})
	if err != nil {
		processes.Close()
		return nil, fmt.Errorf("create tool execution service: %w", err)
	}
	return &Services{
		configured: options.Config, project: options.Project, providerName: providerName, provider: provider,
		client: client, modelMessages: options.ModelMessages, registry: registry, toolService: toolService,
		processes: processes, extensions: options.Extensions,
		fileSystemPolicy: options.FileSystemPolicy, permissions: options.Permissions, instructions: options.Instructions,
		visibility: visibility, skillWarnings: options.Extensions.SkillWarnings(), auditCloser: options.AuditCloser,
		budget: DefaultTurnBudget(),
	}, nil
}

func (runtime *Services) ProviderName() string { return runtime.providerName }

func (runtime *Services) NewModelClientSession() (*ModelClientSession, error) {
	if runtime == nil || runtime.client == nil {
		return nil, errors.New("session model client is unavailable")
	}
	return NewModelClientSession(runtime.client)
}

func (runtime *Services) ModelTemperature() float64 {
	if runtime == nil {
		return 0
	}
	return runtime.provider.Temperature
}

func (runtime *Services) TurnBudget() TurnBudget {
	if runtime == nil {
		return TurnBudget{}
	}
	return runtime.budget
}

func (runtime *Services) ExecuteBatchScoped(ctx context.Context, calls []tool.ToolCall, recorder tool.NormalizedCallRecorder, scope tool.ExecutionScope) ([]tool.ToolExecution, error) {
	if runtime == nil || runtime.toolService == nil {
		return nil, errors.New("tool execution service is unavailable")
	}
	return runtime.toolService.ExecuteBatchScoped(ctx, calls, recorder, scope)
}

func (runtime *Services) ModelInfo() llm.ModelInfo {
	if runtime == nil || runtime.client == nil {
		return llm.ModelInfo{}
	}
	model := runtime.client.Model()
	model.ContextWindow = runtime.provider.ContextWindow
	model.MaxOutputTokens = runtime.provider.MaxOutputTokens
	model.AutoCompactTokenLimit = runtime.provider.AutoCompactTokenLimit
	model.ToolOutputMaxTokens = runtime.provider.ToolOutputMaxTokens
	return model.Normalized()
}

func (runtime *Services) ModelMessages(model llm.ModelInfo) (llm.ModelMessages, error) {
	if runtime == nil {
		return llm.ModelMessages{}, errors.New("model messages runtime is unavailable")
	}
	if model.ModelMessages.HasInstructions() {
		return model.ModelMessages.Normalized(), nil
	}
	if runtime.modelMessages.HasInstructions() {
		return runtime.modelMessages.Normalized(), nil
	}
	return llm.ModelMessages{}, errors.New("model messages are unavailable")
}

func (runtime *Services) AvailableTools() []tool.ToolSpec {
	if runtime == nil || runtime.registry == nil {
		return nil
	}
	entries := runtime.registry.VisibleSnapshot(runtime.visibility)
	result := make([]tool.ToolSpec, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Spec.Clone())
	}
	return result
}

func (runtime *Services) SkillIndex() []skill.IndexEntry {
	if runtime == nil || runtime.extensions == nil || runtime.extensions.Skills() == nil {
		return nil
	}
	return runtime.extensions.Skills().Index()
}

func (runtime *Services) PermissionGrantCount() int {
	if runtime == nil || runtime.permissions == nil {
		return 0
	}
	return runtime.permissions.GrantCount()
}

func (runtime *Services) SkillRevision() string {
	if runtime == nil || runtime.extensions == nil {
		return ""
	}
	return runtime.extensions.SkillRevision()
}

func (runtime *Services) MCPRevision() string {
	if runtime == nil || runtime.extensions == nil {
		return ""
	}
	return runtime.extensions.MCPRevision()
}

func (runtime *Services) Skills() []skill.IndexEntry { return runtime.SkillIndex() }

func (runtime *Services) SetSkillEnabled(name string, enabled bool) error {
	if runtime == nil || runtime.extensions == nil {
		return errors.New("skill catalog is unavailable")
	}
	return runtime.extensions.SetSkillEnabled(name, enabled)
}

func (runtime *Services) MCPServers() []string {
	if runtime == nil || runtime.extensions == nil || runtime.extensions.MCP() == nil {
		return nil
	}
	return runtime.extensions.MCP().EnabledServers()
}

func (runtime *Services) MCPBindings() mcp.BindingSnapshot {
	if runtime == nil || runtime.extensions == nil {
		return mcp.BindingSnapshot{}
	}
	return runtime.extensions.MCPBinding()
}

func (runtime *Services) MCPTools(ctx context.Context, server string) ([]mcp.RemoteTool, error) {
	if runtime == nil || runtime.extensions == nil || runtime.extensions.MCP() == nil {
		return nil, errors.New("MCP manager is unavailable")
	}
	return runtime.extensions.MCP().ListTools(ctx, server)
}

func (runtime *Services) RefreshMCP(ctx context.Context) []error {
	if runtime == nil || runtime.extensions == nil || runtime.extensions.MCP() == nil || runtime.registry == nil {
		return nil
	}
	warnings := make([]error, 0)
	for _, server := range runtime.extensions.MCP().EnabledServers() {
		adapters, err := runtime.extensions.MCP().ToolAdapters(ctx, server, mcp.AdapterOptions{})
		if err != nil {
			warnings = append(warnings, fmt.Errorf("refresh MCP server %q: %w", server, err))
			continue
		}
		definitions := make([]tool.ToolDefinition, len(adapters))
		for index, adapter := range adapters {
			definitions[index] = adapter
		}
		if err := runtime.registry.ReplaceDefinitionGroupWithRegistration("mcp:"+server, definitions, tool.Registration{Exposure: tool.ExposureDeferred, Condition: "mcp.catalog"}); err != nil {
			warnings = append(warnings, fmt.Errorf("register MCP server %q tools: %w", server, err))
		}
	}
	return warnings
}

func (runtime *Services) SkillWarnings() []error {
	if runtime == nil {
		return nil
	}
	return append([]error(nil), runtime.skillWarnings...)
}

func (runtime *Services) Close() error {
	if runtime == nil {
		return nil
	}
	runtime.closeOnce.Do(func() {
		if runtime.processes != nil {
			runtime.processes.Close()
		}
		if runtime.extensions != nil {
			runtime.closeErr = errors.Join(runtime.closeErr, runtime.extensions.Close())
		}
		if runtime.auditCloser != nil {
			runtime.closeErr = errors.Join(runtime.closeErr, runtime.auditCloser.Close())
		}
		if runtime.permissions != nil {
			runtime.permissions.Clear()
		}
	})
	return runtime.closeErr
}

func DefaultClientFactory(providerName string, provider config.ProviderConfig) (llm.Client, error) {
	return openaiadapter.NewAdapter(providerName, provider)
}
