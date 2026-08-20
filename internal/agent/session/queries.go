package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/skill"
)

func (session *Session) PermissionGrantCount() int {
	if session == nil {
		return 0
	}
	return session.services.PermissionGrantCount()
}

func (session *Session) SkillRevision() string {
	if session == nil {
		return ""
	}
	return session.services.SkillRevision()
}

func (session *Session) MCPRevision() string {
	if session == nil {
		return ""
	}
	return session.services.MCPRevision()
}

func (session *Session) Skills() []skill.SkillMetadata {
	if session == nil {
		return nil
	}
	return session.services.Skills()
}

func (session *Session) SetSkillEnabled(name string, enabled bool) error {
	if session == nil {
		return errors.New("session is unavailable")
	}
	return session.services.SetSkillEnabled(name, enabled)
}

func (session *Session) MCPConfiguration() mcp.Config {
	if session == nil {
		return mcp.Config{Servers: map[string]mcp.ServerConfig{}}
	}
	return session.services.MCPConfiguration()
}

func (session *Session) MCPTools(ctx context.Context, server string) (mcp.ToolCatalog, error) {
	if session == nil {
		return mcp.ToolCatalog{}, errors.New("session is unavailable")
	}
	return session.services.MCPTools(ctx, server)
}

func (session *Session) MCPResources(ctx context.Context, server string) (mcp.ResourceCatalog, error) {
	if session == nil {
		return mcp.ResourceCatalog{}, errors.New("session is unavailable")
	}
	return session.services.MCPResources(ctx, server)
}
