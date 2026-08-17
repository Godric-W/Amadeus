package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/instruction"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type InstructionScope interface {
	StepInstructionScope
	Initialize(context.Context, string) (instruction.ResolveRequest, error)
}

func (runtime *Services) PrepareTurn(ctx context.Context, goal string, turnContext *turn.TurnContext, scope InstructionScope, contextUpdate func(agentcontext.UpdateKey) string, appendItems func(context.Context, turn.ID, ...rollout.Item) error) error {
	if runtime == nil || turnContext == nil || scope == nil || contextUpdate == nil || appendItems == nil {
		return fmt.Errorf("turn context preparation is incomplete")
	}
	tools := runtime.AvailableTools()
	if turnContext.Mode == turn.ModeKindPlan {
		tools = planModeTools(tools)
	}
	toolNames := make([]string, len(tools))
	for index, spec := range tools {
		toolNames[index] = spec.Name
	}
	modelMessages, err := runtime.ModelMessages(runtime.ModelInfo())
	if err != nil {
		return err
	}
	developer, err := internalprompt.RenderCollaborationInstructions(modelMessages, turnContext.Mode, toolNames)
	if err != nil {
		return err
	}
	contextItems := make([]rollout.Item, 0, 6)
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
			item, err := rollout.NewItem(rollout.KindContextUpdate, rollout.ContextUpdate{Key: string(key)})
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
		item, err := rollout.NewItem(rollout.KindContextUpdate, rollout.ContextUpdate{
			Key: string(key), Content: rendered, Revision: fragment.Revision(),
		})
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
	effective := runtime.fileSystemPolicy.EffectiveProfile()
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
		DeniedRoots: effective.DeniedRoots, ApprovalCount: runtime.permissions.GrantCount(),
	}
	encodedPermission, err := json.Marshal(permissionPayload)
	if err != nil {
		return err
	}
	if err := setUpdate(agentcontext.UpdatePermissionMode, "## Permission And Isolation Context\n\nPermission context (enforced by runtime, not by this text): "+string(encodedPermission)); err != nil {
		return err
	}
	skillParts := make([]string, 0)
	if runtime.extensions != nil {
		injections, err := runtime.extensions.ResolveSkillInjections(goal)
		if err != nil {
			return err
		}
		for _, injection := range injections {
			skillParts = append(skillParts, "{\"type\":\"amadeus.skill_injection.v1\",\"name\":\""+injection.Name+"\"}\n"+injection.Content)
		}
		if err := setUpdate(agentcontext.UpdateMCP, "MCP tools are available only through their exposed Tool Specs and current bindings."); err != nil {
			return err
		}
	} else if err := setUpdate(agentcontext.UpdateMCP, ""); err != nil {
		return err
	}
	indexParts := make([]string, 0)
	for _, entry := range runtime.SkillIndex() {
		if entry.Enabled {
			indexParts = append(indexParts, fmt.Sprintf("- `%s`: %s", entry.Name, entry.Description))
		}
	}
	if len(indexParts) > 0 {
		skillParts = append([]string{"## Skills And Extensions\n\n{\"type\":\"amadeus.skill_index.v1\",\"skills\":\n" + strings.Join(indexParts, "\n")}, skillParts...)
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
