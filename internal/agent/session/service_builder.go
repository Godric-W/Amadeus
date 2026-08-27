package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agentcompact "github.com/Godric-W/Amadeus/internal/agent/compact"
	"github.com/Godric-W/Amadeus/internal/agentsmd"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/tool"
)

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
	if adapters.ClientFactory == nil || adapters.AuditFactory == nil || !adapters.ModelMessages.HasInstructions() {
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
	providerName := configuration.Runtime.ModelProvider
	provider := configuration.Runtime.ModelProviders[providerName]
	client, err := adapters.ClientFactory(providerName, configuration.Runtime.Model, provider)
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
	modelInfo.InputModalities = append([]llm.InputModality(nil), configuration.Runtime.ModelInputModalities...)
	modelInfo.SupportsOriginalImageDetail = configuration.Runtime.ModelSupportsOriginalImageDetail
	modelInfo = modelInfo.Normalized()
	client, err = llm.WithModelInfo(client, modelInfo)
	if err != nil {
		return SessionServices{}, fmt.Errorf("configure provider model client: %w", err)
	}
	var approvalPort policy.ApprovalPort
	if configuration.Source.IsSubAgent() {
		approvalPort = denySubagentApprovalPort{}
	} else {
		approvalPort, err = newSessionApprovalPort(owner)
		if err != nil {
			return SessionServices{}, err
		}
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
	toolRuntime, err := BuildToolRuntime(ToolRuntimeOptions{
		Config: configuration.Runtime, Project: root, Client: client, ModelInfo: modelInfo, Events: owner,
		Audit: auditSink, Skills: skills, MCP: mcpRuntime, WebFetcher: adapters.WebFetcher,
		WebSearch: adapters.WebSearch, FileSystemPolicy: fileSystem,
		AgentControl: base.AgentControl, SessionSource: configuration.Source,
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
	base.compaction = &agentcompact.Service{ModelInfo: modelInfo, Assets: adapters.CompactionAssets}
	base.fileSystem = fileSystem
	base.visibility = toolRuntime.Visibility
	base.source = configuration.Source.Clone()
	base.skillWarnings = append([]error(nil), skillWarnings...)
	base.auditCloser = auditCloser
	base.budget = configuredTurnBudget(configuration)
	base.closeState = &sessionServicesCloseState{}
	cleanupMCP = false
	return base, nil
}

func configuredTurnBudget(configuration Configuration) TurnBudget {
	if !configuration.Source.IsSubAgent() {
		return DefaultTurnBudget()
	}
	multiAgent := configuration.Runtime.Agent.MultiAgent
	return TurnBudget{
		MaxSamples: multiAgent.ChildMaxSamples, MaxToolCalls: multiAgent.ChildMaxToolCalls,
		MaxDuration: multiAgent.ChildMaxDuration, WarnRatio: 0.9,
	}
}

type denySubagentApprovalPort struct{}

func (denySubagentApprovalPort) Decide(context.Context, policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	return policy.ApprovalDecision{
		Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourcePolicy,
		Reason: "sub-agent tools cannot request interactive approval",
	}, nil
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
