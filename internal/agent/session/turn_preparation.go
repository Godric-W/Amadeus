package session

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/skill"
)

func (services *SessionServices) PrepareTurn(ctx context.Context, goal string, turnContext *turn.TurnContext, scope engine.InstructionScope, contextUpdate func(agentcontext.UpdateKey) string, appendItems func(context.Context, protocol.TurnID, ...rollout.RolloutItem) error) error {
	if services == nil || turnContext == nil || scope == nil || contextUpdate == nil || appendItems == nil {
		return fmt.Errorf("turn context preparation is incomplete")
	}
	tools := services.AvailableTools()
	if turnContext.Mode == turn.ModeKindPlan {
		tools = engine.PlanModeTools(tools)
	}
	toolNames := make([]string, len(tools))
	for index, spec := range tools {
		toolNames[index] = spec.Name
	}
	modelMessages, err := services.ModelMessages(services.ModelInfo())
	if err != nil {
		return err
	}
	developer, err := internalprompt.RenderCollaborationInstructions(modelMessages, turnContext.Mode, toolNames)
	if err != nil {
		return err
	}
	contextItems := make([]rollout.RolloutItem, 0, 6)
	worldState := agentcontext.NewWorldState()
	setUpdate := func(key agentcontext.UpdateKey, content string) error {
		if err := worldState.Set(key, content); err != nil {
			return err
		}
		fragment := worldState.Fragment(key)
		if fragment.Render() == "" {
			if contextUpdate(key) == "" {
				return nil
			}
			item, err := rollout.NewEventMsgItem(protocol.ContextUpdateEvent{Key: string(key)})
			if err != nil {
				return err
			}
			contextItems = append(contextItems, item)
			return nil
		}
		rendered := fragment.Render()
		if contextUpdate(key) == rendered {
			return nil
		}
		item, err := rollout.NewEventMsgItem(protocol.ContextUpdateEvent{Key: string(key), Content: rendered, Revision: fragment.Revision()})
		if err != nil {
			return err
		}
		contextItems = append(contextItems, item)
		return nil
	}
	if err := setUpdate(agentcontext.UpdateCollaborationMode, developer); err != nil {
		return err
	}
	request, err := scope.Initialize(ctx, turnContext.CWD)
	if err != nil {
		return err
	}
	if err := setUpdate(agentcontext.UpdateEnvironment, fmt.Sprintf("<cwd>%s</cwd>\n<instruction_target>%s</instruction_target>", html.EscapeString(turnContext.CWD), html.EscapeString(request.TargetPath))); err != nil {
		return err
	}
	effective := services.fileSystem.EffectiveProfile()
	permissionPayload := struct {
		ReadHost       bool     `json:"read_host"`
		WorkspaceRoots []string `json:"workspace_roots"`
		TemporaryRoots []string `json:"temporary_roots"`
		ReadOnlyRoots  []string `json:"read_only_roots"`
		DeniedRoots    []string `json:"denied_roots"`
		ApprovalCount  int      `json:"session_command_approval_count"`
	}{
		ReadHost: effective.ReadHost, WorkspaceRoots: effective.WorkspaceRoots,
		TemporaryRoots: effective.TemporaryRoots, ReadOnlyRoots: effective.ReadOnlyRoots,
		DeniedRoots: effective.DeniedRoots, ApprovalCount: services.permissions.GrantCount(),
	}
	encodedPermission, err := json.Marshal(permissionPayload)
	if err != nil {
		return err
	}
	if err := setUpdate(agentcontext.UpdatePermissionMode, "## Permission And Isolation Context\n\nPermission context (enforced by runtime, not by this text): "+string(encodedPermission)); err != nil {
		return err
	}
	skillParts := make([]string, 0)
	if services.skills != nil {
		documents, resolveErr := services.skills.ResolveExplicit(goal)
		if resolveErr != nil {
			return resolveErr
		}
		for _, document := range documents {
			encoded, encodeErr := json.Marshal(struct {
				Type     string `json:"type"`
				Name     string `json:"name"`
				Path     string `json:"path"`
				Revision string `json:"revision"`
			}{Type: "amadeus.skill_injection.v2", Name: document.Name, Path: document.PathToSkillMD, Revision: document.Revision})
			if encodeErr != nil {
				return encodeErr
			}
			skillParts = append(skillParts, string(encoded)+"\n"+strings.TrimSpace(document.Content))
		}
	}
	if services.mcp != nil {
		if err := setUpdate(agentcontext.UpdateMCP, "MCP tools are available only through their exposed Tool Specs and current bindings."); err != nil {
			return err
		}
	} else if err := setUpdate(agentcontext.UpdateMCP, ""); err != nil {
		return err
	}
	index := services.SkillIndex()
	indexParts := make([]string, 0, len(index))
	for _, entry := range index {
		if entry.Enabled {
			indexParts = append(indexParts, fmt.Sprintf("- `%s`: %s", entry.Name, entry.Description))
		}
	}
	if len(indexParts) > 0 {
		encoded, encodeErr := json.Marshal(struct {
			Type   string                `json:"type"`
			Skills []skill.SkillMetadata `json:"skills"`
		}{Type: "amadeus.skill_index.v2", Skills: index})
		if encodeErr != nil {
			return encodeErr
		}
		skillParts = append([]string{"## Skills And Extensions\n\n" + string(encoded)}, skillParts...)
	} else if len(skillParts) > 0 {
		skillParts = append([]string{"## Skills And Extensions"}, skillParts...)
	} else {
		skillParts = []string{"## Skills And Extensions\n\nNo Skills are currently available."}
	}
	if err := setUpdate(agentcontext.UpdateSkills, strings.Join(skillParts, "\n\n")); err != nil {
		return err
	}
	if len(contextItems) > 0 {
		if err := appendItems(ctx, turnContext.TurnID, contextItems...); err != nil {
			return fmt.Errorf("persist dynamic context updates: %w", err)
		}
	}
	return nil
}
