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

type ClientFactory func(string, string, config.ModelProviderInfo) (llm.Client, error)

type ServicesOptions struct {
	Config            config.Config
	Project           project.Root
	ClientFactory     ClientFactory
	Events            protocol.EventSink
	Approvals         policy.ApprovalPort
	PlanUpdater       builtin.PlanUpdater
	Audit             audit.Sink
	AuditCloser       io.Closer
	ExtensionAssembly *extensionruntime.Assembly
	WebFetcher        webfetch.Fetcher
	WebSearch         websearch.Provider
	FileSystemPolicy  *project.FileSystemPolicy
	Permissions       *policy.SessionPermissionContext
	Instructions      *instruction.WorkspaceResolver
	ModelMessages     llm.ModelMessages
}

// Services owns the capabilities shared by every Turn in one Session.
// Turn-specific state is captured by StepContext and never stored here.
type Services struct {
	configured        config.Config
	project           project.Root
	providerName      string
	provider          config.ModelProviderInfo
	modelInfo         llm.ModelInfo
	client            llm.Client
	modelMessages     llm.ModelMessages
	registry          *tool.Registry
	toolService       *tool.ToolExecutionService
	processes         *processdomain.Manager
	extensionAssembly *extensionruntime.Assembly
	fileSystemPolicy  *project.FileSystemPolicy
	permissions       *policy.SessionPermissionContext
	instructions      *instruction.WorkspaceResolver
	visibility        map[string]bool
	skillWarnings     []error
	budget            TurnBudget
	auditCloser       io.Closer
	closeOnce         sync.Once
	closeErr          error
}

func NewServices(options ServicesOptions) (*Services, error) {
	if err := config.Validate(options.Config); err != nil {
		return nil, fmt.Errorf("validate services configuration: %w", err)
	}
	if options.Project.Path() == "" || options.Events == nil || options.Approvals == nil || options.PlanUpdater == nil || options.Audit == nil {
		return nil, errors.New("services composition is incomplete")
	}
	if options.ExtensionAssembly == nil || options.FileSystemPolicy == nil || options.Permissions == nil || options.Instructions == nil {
		return nil, errors.New("session services are incomplete")
	}
	if !options.ModelMessages.HasInstructions() {
		return nil, errors.New("services model messages are empty")
	}
	createClient := options.ClientFactory
	if createClient == nil {
		createClient = DefaultClientFactory
	}
	providerName := options.Config.ModelProvider
	provider := options.Config.ModelProviders[providerName]
	client, err := createClient(providerName, options.Config.Model, provider)
	if err != nil {
		return nil, fmt.Errorf("create provider client: %w", err)
	}
	if client == nil {
		return nil, errors.New("create provider client: factory returned nil")
	}
	modelInfo := client.Model()
	modelInfo.Provider = providerName
	modelInfo.Name = options.Config.Model
	modelInfo.ContextWindow = options.Config.ModelContextWindow
	modelInfo.AutoCompactTokenLimit = options.Config.ModelAutoCompactTokenLimit
	modelInfo.ToolOutputTokenLimit = options.Config.ToolOutputTokenLimit
	modelInfo = modelInfo.Normalized()
	coordinator, err := policy.NewApprovalCoordinator(options.Approvals)
	if err != nil {
		return nil, fmt.Errorf("create approval coordinator: %w", err)
	}
	registry, processes, visibility, err := buildToolRuntime(toolRuntimeOptions{
		configured: options.Config, project: options.Project, client: client, events: options.Events,
		planUpdater: options.PlanUpdater, audit: options.Audit, extensionAssembly: options.ExtensionAssembly,
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
		modelInfo: modelInfo, client: client, modelMessages: options.ModelMessages, registry: registry, toolService: toolService,
		processes: processes, extensionAssembly: options.ExtensionAssembly,
		fileSystemPolicy: options.FileSystemPolicy, permissions: options.Permissions, instructions: options.Instructions,
		visibility: visibility, skillWarnings: options.ExtensionAssembly.SkillWarnings(), auditCloser: options.AuditCloser,
		budget: DefaultTurnBudget(),
	}, nil
}

func (runtime *Services) ProviderName() string { return runtime.providerName }

func (runtime *Services) NewModelClientSession() (*ModelClientSession, error) {
	if runtime == nil || runtime.client == nil {
		return nil, errors.New("session model client is unavailable")
	}
	return NewModelClientSession(runtime.client, ModelClientSessionConfig{
		StreamMaxRetries:  runtime.provider.StreamMaxRetries,
		StreamIdleTimeout: runtime.provider.StreamIdleTimeout,
	})
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
	if runtime == nil {
		return llm.ModelInfo{}
	}
	return runtime.modelInfo
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

func (runtime *Services) SkillIndex() []skill.SkillMetadata {
	if runtime == nil || runtime.extensionAssembly == nil || runtime.extensionAssembly.SkillCatalog() == nil {
		return nil
	}
	return runtime.extensionAssembly.SkillCatalog().Index()
}

func (runtime *Services) PermissionGrantCount() int {
	if runtime == nil || runtime.permissions == nil {
		return 0
	}
	return runtime.permissions.GrantCount()
}

func (runtime *Services) SkillRevision() string {
	if runtime == nil || runtime.extensionAssembly == nil {
		return ""
	}
	return runtime.extensionAssembly.SkillRevision()
}

func (runtime *Services) MCPRevision() string {
	if runtime == nil || runtime.extensionAssembly == nil {
		return ""
	}
	return runtime.extensionAssembly.MCPRevision()
}

func (runtime *Services) Skills() []skill.SkillMetadata { return runtime.SkillIndex() }

func (runtime *Services) SetSkillEnabled(name string, enabled bool) error {
	if runtime == nil || runtime.extensionAssembly == nil {
		return errors.New("skill catalog is unavailable")
	}
	return runtime.extensionAssembly.SetSkillEnabled(name, enabled)
}

func (runtime *Services) MCPConfiguration() mcp.Config {
	if runtime == nil || runtime.extensionAssembly == nil {
		return mcp.Config{Servers: map[string]mcp.ServerConfig{}}
	}
	return runtime.extensionAssembly.MCPConfiguration()
}

func (runtime *Services) MCPTools(ctx context.Context, server string) (mcp.ToolCatalog, error) {
	if runtime == nil || runtime.extensionAssembly == nil || runtime.extensionAssembly.MCPRuntime() == nil {
		return mcp.ToolCatalog{}, errors.New("MCP runtime is unavailable")
	}
	return runtime.extensionAssembly.MCPRuntime().ToolCatalog(ctx, server)
}

func (runtime *Services) MCPResources(ctx context.Context, server string) (mcp.ResourceCatalog, error) {
	if runtime == nil || runtime.extensionAssembly == nil || runtime.extensionAssembly.MCPRuntime() == nil {
		return mcp.ResourceCatalog{}, errors.New("MCP runtime is unavailable")
	}
	return runtime.extensionAssembly.MCPRuntime().ResourceCatalog(ctx, server)
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
		if runtime.extensionAssembly != nil {
			runtime.closeErr = errors.Join(runtime.closeErr, runtime.extensionAssembly.Close())
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

func DefaultClientFactory(providerName, model string, provider config.ModelProviderInfo) (llm.Client, error) {
	return openaiadapter.NewAdapter(providerName, model, provider)
}
