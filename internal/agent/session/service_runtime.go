package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func (services *SessionServices) NewModelClientSession() (*engine.ModelClientSession, error) {
	if services == nil || services.modelClient == nil {
		return nil, errors.New("session model client is unavailable")
	}
	return engine.NewModelClientSession(services.modelClient, engine.ModelClientSessionConfig{
		StreamMaxRetries:  services.provider.StreamMaxRetries,
		StreamIdleTimeout: services.provider.StreamIdleTimeout,
	})
}

func (services *SessionServices) TurnBudget() engine.TurnBudget {
	if services == nil {
		return engine.TurnBudget{}
	}
	return services.budget
}

func (services *SessionServices) ExecuteBatchScoped(ctx context.Context, calls []tool.ToolCall, recorder tool.NormalizedCallRecorder, scope tool.ExecutionScope) ([]tool.ToolExecution, error) {
	if services == nil || services.toolExecutor == nil {
		return nil, errors.New("tool execution service is unavailable")
	}
	return services.toolExecutor.ExecuteBatchScoped(ctx, calls, recorder, scope)
}

func (services *SessionServices) ModelInfo() llm.ModelInfo {
	if services == nil {
		return llm.ModelInfo{}
	}
	return services.modelInfo
}

func (services *SessionServices) ModelMessages(model llm.ModelInfo) (llm.ModelMessages, error) {
	if services == nil {
		return llm.ModelMessages{}, errors.New("model messages are unavailable")
	}
	if model.ModelMessages.HasInstructions() {
		return model.ModelMessages.Normalized(), nil
	}
	if services.modelMessages.HasInstructions() {
		return services.modelMessages.Normalized(), nil
	}
	return llm.ModelMessages{}, errors.New("model messages are unavailable")
}

func (services *SessionServices) AvailableTools() []tool.ToolSpec {
	if services == nil || services.tools == nil {
		return nil
	}
	entries := services.tools.VisibleSnapshot(services.visibility)
	result := make([]tool.ToolSpec, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Spec.Clone())
	}
	return result
}

func (services *SessionServices) SkillIndex() []skill.SkillMetadata {
	if services == nil || services.skills == nil {
		return nil
	}
	return services.skills.Index()
}

func (services *SessionServices) PermissionGrantCount() int {
	if services == nil || services.permissions == nil {
		return 0
	}
	return services.permissions.GrantCount()
}

func (services *SessionServices) SkillRevision() string {
	if services == nil || services.skills == nil {
		return ""
	}
	revision, _ := services.skills.Revision()
	return revision
}

func (services *SessionServices) MCPRevision() string {
	if services == nil || services.mcp == nil {
		return ""
	}
	return services.mcp.Revision()
}

func (services *SessionServices) Skills() []skill.SkillMetadata { return services.SkillIndex() }

func (services *SessionServices) SetSkillEnabled(name string, enabled bool) error {
	if services == nil || services.skills == nil {
		return errors.New("skill catalog is unavailable")
	}
	return services.skills.SetEnabled(name, enabled)
}

func (services *SessionServices) MCPConfiguration() mcp.Config {
	if services == nil || services.mcp == nil {
		return mcp.Config{Servers: map[string]mcp.ServerConfig{}}
	}
	return services.mcp.Configuration()
}

func (services *SessionServices) MCPTools(ctx context.Context, server string) (mcp.ToolCatalog, error) {
	if services == nil || services.mcp == nil {
		return mcp.ToolCatalog{}, errors.New("MCP runtime is unavailable")
	}
	return services.mcp.ToolCatalog(ctx, server)
}

func (services *SessionServices) MCPResources(ctx context.Context, server string) (mcp.ResourceCatalog, error) {
	if services == nil || services.mcp == nil {
		return mcp.ResourceCatalog{}, errors.New("MCP runtime is unavailable")
	}
	return services.mcp.ResourceCatalog(ctx, server)
}

func (services *SessionServices) SkillWarnings() []error {
	if services == nil {
		return nil
	}
	return append([]error(nil), services.skillWarnings...)
}

func (services *SessionServices) Compact(ctx context.Context, request engine.CompactRequest) ([]rollout.RolloutItem, error) {
	if services == nil || services.compactor == nil {
		return nil, errors.New("compactor is unavailable")
	}
	return services.compactor.Compact(ctx, request)
}
