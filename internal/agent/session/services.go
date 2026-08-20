package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agentsmd"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	openaiadapter "github.com/Godric-W/Amadeus/internal/llm/openai"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/policy"
	processdomain "github.com/Godric-W/Amadeus/internal/process"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/thread"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type ClientFactory func(string, string, config.ModelProviderInfo) (llm.Client, error)

type AuditFactory func() (audit.Sink, io.Closer, error)

type ServiceAdapters struct {
	ClientFactory    ClientFactory
	MCPClientFactory mcp.ClientFactory
	WebFetcher       webfetch.Fetcher
	WebSearch        websearch.Provider
	AuditFactory     AuditFactory
	ModelMessages    llm.ModelMessages
}

func (adapters ServiceAdapters) configured() bool {
	return adapters.AuditFactory != nil || adapters.ClientFactory != nil || adapters.ModelMessages.HasInstructions()
}

type SessionServices struct {
	LiveThread *thread.LiveThread
	Clock      func() time.Time
	NextID     func(string) string

	modelClient   llm.Client
	provider      config.ModelProviderInfo
	modelInfo     llm.ModelInfo
	modelMessages llm.ModelMessages
	tools         *tool.Registry
	toolExecutor  *tool.ToolExecutionService
	processes     *processdomain.Manager
	agentsMd      *agentsmd.AgentsMdManager
	skills        *skill.SkillCatalog
	mcp           *mcp.MCPRuntime
	webFetcher    webfetch.Fetcher
	webSearch     websearch.Provider
	approvals     policy.ApprovalPort
	permissions   *policy.SessionPermissionContext
	compactor     *engine.Compactor
	fileSystem    *project.FileSystemPolicy
	visibility    map[string]bool
	skillWarnings []error
	auditCloser   io.Closer
	budget        engine.TurnBudget

	closeState *sessionServicesCloseState
}

type sessionServicesCloseState struct {
	once sync.Once
	err  error
}

func buildSessionServices(ctx context.Context, owner *Session, base SessionServices, adapters ServiceAdapters) (SessionServices, error) {
	if ctx == nil || owner == nil {
		return SessionServices{}, errors.New("session services construction is incomplete")
	}
	configuration := owner.state.Configuration
	if err := config.Validate(configuration.Runtime); err != nil {
		return SessionServices{}, fmt.Errorf("validate session configuration: %w", err)
	}
	root, err := project.NewRoot(configuration.CWD)
	if err != nil {
		return SessionServices{}, err
	}
	if adapters.AuditFactory == nil || !adapters.ModelMessages.HasInstructions() {
		return SessionServices{}, errors.New("session service adapters are incomplete")
	}
	workspaceRoots := append([]string{root.Path()}, configuration.WorkspaceRoots...)
	fileSystem, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{
		CWD: root.Path(),
		Profile: project.PermissionProfile{
			ReadHost: true, WorkspaceRoots: workspaceRoots, TemporaryRoots: project.DefaultTemporaryRoots(),
			ReadOnlyRoots: workspaceReadOnlyRoots(workspaceRoots),
			DeniedRoots:   append(project.DefaultDeniedRoots(), amadeusDeniedRoots(configuration.AmadeusRoot)...),
		},
	})
	if err != nil {
		return SessionServices{}, err
	}
	roots := []project.Root{root}
	for _, workspaceRoot := range configuration.WorkspaceRoots {
		resolved, resolveErr := project.NewRoot(workspaceRoot)
		if resolveErr != nil {
			return SessionServices{}, resolveErr
		}
		roots = append(roots, resolved)
	}
	agentsMd, err := agentsmd.NewManager(configuration.AmadeusRoot, roots, agentsmd.Options{})
	if err != nil {
		return SessionServices{}, err
	}
	skills, skillWarnings, err := skill.Load(configuration.AmadeusRoot, root, skill.DefaultLoadOptions())
	if err != nil {
		return SessionServices{}, err
	}
	mcpConfig, err := mcp.Load(configuration.AmadeusRoot, root, mcp.LoadOptions{})
	if err != nil {
		return SessionServices{}, err
	}
	mcpRuntime, err := mcp.NewMCPRuntime(mcpConfig, adapters.MCPClientFactory)
	if err != nil {
		return SessionServices{}, err
	}
	cleanupMCP := true
	defer func() {
		if cleanupMCP {
			_ = mcpRuntime.Close()
		}
	}()
	createClient := adapters.ClientFactory
	if createClient == nil {
		createClient = func(providerName, model string, provider config.ModelProviderInfo) (llm.Client, error) {
			return openaiadapter.NewAdapter(providerName, model, provider)
		}
	}
	providerName := configuration.Runtime.ModelProvider
	provider := configuration.Runtime.ModelProviders[providerName]
	client, err := createClient(providerName, configuration.Runtime.Model, provider)
	if err != nil {
		return SessionServices{}, fmt.Errorf("create provider client: %w", err)
	}
	if client == nil {
		return SessionServices{}, errors.New("create provider client: factory returned nil")
	}
	modelInfo := client.Model()
	modelInfo.Provider = providerName
	modelInfo.Name = configuration.Runtime.Model
	modelInfo.ContextWindow = configuration.Runtime.ModelContextWindow
	modelInfo.AutoCompactTokenLimit = configuration.Runtime.ModelAutoCompactTokenLimit
	modelInfo.ToolOutputTokenLimit = configuration.Runtime.ToolOutputTokenLimit
	modelInfo = modelInfo.Normalized()
	approvalPort, err := newSessionApprovalPort(owner)
	if err != nil {
		return SessionServices{}, err
	}
	coordinator, err := policy.NewApprovalCoordinator(approvalPort)
	if err != nil {
		return SessionServices{}, fmt.Errorf("create approval coordinator: %w", err)
	}
	auditSink, auditCloser, err := adapters.AuditFactory()
	if err != nil {
		return SessionServices{}, err
	}
	if auditSink == nil {
		if auditCloser != nil {
			_ = auditCloser.Close()
		}
		return SessionServices{}, errors.New("session audit factory returned nil sink")
	}
	toolRuntime, err := engine.BuildToolRuntime(engine.ToolRuntimeOptions{
		Config: configuration.Runtime, Project: root, Client: client, Events: owner,
		Audit: auditSink, Skills: skills, MCP: mcpRuntime, WebFetcher: adapters.WebFetcher,
		WebSearch: adapters.WebSearch, FileSystemPolicy: fileSystem,
	})
	if err != nil {
		if auditCloser != nil {
			_ = auditCloser.Close()
		}
		return SessionServices{}, err
	}
	permissions := policy.NewSessionPermissionContext()
	toolExecutor, err := tool.NewToolExecutionService(toolRuntime.Registry, tool.NewArgumentValidator(), tool.ToolExecutionServiceOptions{
		MaxParallel: configuration.Runtime.Agent.MaxParallelTools, Visibility: toolRuntime.Visibility,
		Approvals: coordinator, Permissions: permissions, TargetObserver: agentsMd,
		Interactions: owner,
	})
	if err != nil {
		toolRuntime.Processes.Close()
		if auditCloser != nil {
			_ = auditCloser.Close()
		}
		return SessionServices{}, fmt.Errorf("create tool execution service: %w", err)
	}
	base.modelClient = client
	base.provider = provider
	base.modelInfo = modelInfo
	base.modelMessages = adapters.ModelMessages.Normalized()
	base.tools = toolRuntime.Registry
	base.toolExecutor = toolExecutor
	base.processes = toolRuntime.Processes
	base.agentsMd = agentsMd
	base.skills = skills
	base.mcp = mcpRuntime
	base.webFetcher = toolRuntime.WebFetcher
	base.webSearch = toolRuntime.WebSearch
	base.approvals = approvalPort
	base.permissions = permissions
	base.compactor = &engine.Compactor{ProviderName: providerName, ModelInfo: modelInfo, ModelMessages: base.modelMessages}
	base.fileSystem = fileSystem
	base.visibility = toolRuntime.Visibility
	base.skillWarnings = append([]error(nil), skillWarnings...)
	base.auditCloser = auditCloser
	base.budget = engine.DefaultTurnBudget()
	base.closeState = &sessionServicesCloseState{}
	cleanupMCP = false
	return base, nil
}

func (services *SessionServices) Close() error {
	if services == nil {
		return nil
	}
	if services.closeState == nil {
		services.closeState = &sessionServicesCloseState{}
	}
	state := services.closeState
	state.once.Do(func() {
		if services.processes != nil {
			services.processes.Close()
		}
		if services.mcp != nil {
			state.err = errors.Join(state.err, services.mcp.Close())
		}
		if services.auditCloser != nil {
			state.err = errors.Join(state.err, services.auditCloser.Close())
		}
		if services.permissions != nil {
			services.permissions.Clear()
		}
	})
	return state.err
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
