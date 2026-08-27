package threadmanager

import (
	"context"
	"errors"

	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/skill"
)

func (threadRuntime *AmadeusThread) PromptDiagnostics() agentsession.PromptDiagnostics {
	if threadRuntime == nil || threadRuntime.session == nil {
		return agentsession.PromptDiagnostics{}
	}
	return threadRuntime.session.PromptDiagnostics()
}

func (threadRuntime *AmadeusThread) PermissionGrantCount() int {
	if threadRuntime == nil || threadRuntime.session == nil {
		return 0
	}
	return threadRuntime.session.PermissionGrantCount()
}

func (threadRuntime *AmadeusThread) SkillRevision() string {
	if threadRuntime == nil || threadRuntime.session == nil {
		return ""
	}
	return threadRuntime.session.SkillRevision()
}

func (threadRuntime *AmadeusThread) MCPRevision() string {
	if threadRuntime == nil || threadRuntime.session == nil {
		return ""
	}
	return threadRuntime.session.MCPRevision()
}

func (threadRuntime *AmadeusThread) Skills() []skill.SkillMetadata {
	if threadRuntime == nil || threadRuntime.session == nil {
		return nil
	}
	return threadRuntime.session.Skills()
}

func (threadRuntime *AmadeusThread) SetSkillEnabled(name string, enabled bool) error {
	if threadRuntime == nil || threadRuntime.session == nil {
		return errors.New("thread session is unavailable")
	}
	return threadRuntime.session.SetSkillEnabled(name, enabled)
}

func (threadRuntime *AmadeusThread) MCPConfiguration() mcp.Config {
	if threadRuntime == nil || threadRuntime.session == nil {
		return mcp.Config{Servers: map[string]mcp.ServerConfig{}}
	}
	return threadRuntime.session.MCPConfiguration()
}

func (threadRuntime *AmadeusThread) MCPTools(ctx context.Context, server string) (mcp.ToolCatalog, error) {
	if threadRuntime == nil || threadRuntime.session == nil {
		return mcp.ToolCatalog{}, errors.New("thread session is unavailable")
	}
	return threadRuntime.session.MCPTools(ctx, server)
}

func (threadRuntime *AmadeusThread) MCPResources(ctx context.Context, server string) (mcp.ResourceCatalog, error) {
	if threadRuntime == nil || threadRuntime.session == nil {
		return mcp.ResourceCatalog{}, errors.New("thread session is unavailable")
	}
	return threadRuntime.session.MCPResources(ctx, server)
}
