package session

import (
	"context"
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/multiagent"
	"github.com/Godric-W/Amadeus/internal/agentsmd"
	"github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type StepContext struct {
	Turn               TurnContext
	ExtensionData      *extension.Data
	Model              llm.ModelInfo
	ToolRouter         tool.ToolRouter
	LoadedAgentsMd     agentsmd.LoadedAgentsMd
	Skills             []skill.SkillMetadata
	PermissionProfile  project.PermissionProfile
	PermissionGrants   int
	Subagents          []multiagent.AgentRecord
	MCPBindingRevision string
	SkillRevision      string
}

func (services *SessionServices) CaptureStep(ctx context.Context, turnContext TurnContext) (StepContext, error) {
	if services == nil {
		return StepContext{}, errors.New("step context capture is incomplete")
	}
	loadedAgentsMd := agentsmd.LoadedAgentsMd{}
	if services.agentsMd != nil {
		loaded, _, err := services.agentsMd.Refresh(ctx, turnContext.CWD)
		if err != nil {
			return StepContext{}, err
		}
		loadedAgentsMd = loaded
	}
	requestSnapshot := tool.RequestSnapshot{}
	var loadedSkills []skill.SkillMetadata
	if services.skills != nil {
		var err error
		loadedSkills, requestSnapshot.SkillRevision, err = services.skills.Snapshot()
		if err != nil {
			return StepContext{}, err
		}
	}
	if services.mcp != nil {
		requestSnapshot.MCPBindingRevision = services.mcp.Binding().Revision
	}
	requestSnapshot.AgentsMdRevision = loadedAgentsMd.Revision
	var include tool.ToolRouteFilter
	if turnContext.Mode == ModeKindPlan {
		include = planModeToolAllowed
	}
	include = composeToolFilters(include, services.source)
	stepID := string(turnContext.TurnID) + ":step"
	if services.NextID != nil {
		stepID = services.NextID("step-extension")
	}
	stepData, err := extension.NewData(stepID)
	if err != nil {
		return StepContext{}, err
	}
	var extensionTools []tool.ToolDefinition
	registry := services.extensionRegistry()
	for _, contributor := range registry.Tools() {
		extensionTools = append(extensionTools, contributor.Tools(services.sessionExtensions, services.threadExtensions, stepData)...)
	}
	router, err := services.tools.SnapshotRouterWithDefinitions(services.visibility, requestSnapshot, include, extensionTools)
	if err != nil {
		return StepContext{}, err
	}
	model := services.ModelInfo()
	permissionProfile := project.PermissionProfile{}
	if services.fileSystem != nil {
		permissionProfile = services.fileSystem.EffectiveProfile()
	}
	permissionGrants := 0
	if services.permissions != nil {
		permissionGrants = services.permissions.GrantCount()
	}
	var subagents []multiagent.AgentRecord
	if !services.source.IsSubAgent() && services.AgentControl != nil {
		subagents = services.AgentControl.SnapshotAll()
	}
	return StepContext{
		Turn: turnContext, ExtensionData: stepData, Model: model, ToolRouter: router, LoadedAgentsMd: loadedAgentsMd,
		Skills: loadedSkills, PermissionProfile: permissionProfile, PermissionGrants: permissionGrants, Subagents: subagents,
		MCPBindingRevision: requestSnapshot.MCPBindingRevision, SkillRevision: requestSnapshot.SkillRevision,
	}, nil
}

func planModeToolAllowed(spec tool.ToolSpec) bool {
	if spec.Name == "update_plan" {
		return false
	}
	if spec.SideEffect == tool.SideEffectNone || spec.SideEffect == tool.SideEffectRead {
		return true
	}
	allowedNetwork := map[string]struct{}{"web_search": {}, "web_fetch": {}, "mcp_list_tools": {}, "mcp_list_resources": {}, "mcp_read_resource": {}}
	_, ok := allowedNetwork[strings.TrimSpace(spec.Name)]
	return ok
}

func composeToolFilters(existing tool.ToolRouteFilter, source protocol.SessionSource) tool.ToolRouteFilter {
	if !source.IsSubAgent() {
		return existing
	}
	return func(spec tool.ToolSpec) bool {
		if existing != nil && !existing(spec) {
			return false
		}
		switch spec.Name {
		case "read", "glob", "grep", "read_skill", "web_search":
			return true
		default:
			return false
		}
	}
}
