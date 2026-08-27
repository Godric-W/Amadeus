package session

import (
	"errors"
	"io"
	"sync"
	"time"

	agentcompact "github.com/Godric-W/Amadeus/internal/agent/compact"
	"github.com/Godric-W/Amadeus/internal/agent/multiagent"
	"github.com/Godric-W/Amadeus/internal/agentsmd"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/policy"
	processdomain "github.com/Godric-W/Amadeus/internal/process"
	"github.com/Godric-W/Amadeus/internal/project"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/threadstore"
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
	CompactionAssets internalprompt.CompactionAssets
}

func (adapters ServiceAdapters) configured() bool {
	return adapters.ClientFactory != nil && adapters.AuditFactory != nil && adapters.ModelMessages.HasInstructions() && adapters.CompactionAssets.Valid()
}

type SessionServices struct {
	LiveThread   *threadstore.LiveThread
	Clock        func() time.Time
	NextID       func(string) string
	AgentControl *multiagent.Control

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
	compaction    *agentcompact.Service
	fileSystem    *project.FileSystemPolicy
	visibility    map[string]bool
	source        protocol.SessionSource
	skillWarnings []error
	auditCloser   io.Closer
	budget        TurnBudget

	closeState *sessionServicesCloseState
}

type sessionServicesCloseState struct {
	once sync.Once
	err  error
}

func (services *SessionServices) Close() error {
	if services == nil {
		return nil
	}
	state := services.closeState
	if state == nil {
		state = &sessionServicesCloseState{}
		services.closeState = state
	}
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
