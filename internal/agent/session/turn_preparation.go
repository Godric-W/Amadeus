package session

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/multiagent"
	contextmanager "github.com/Godric-W/Amadeus/internal/contextmanager"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type preparedContextUpdate struct {
	key     contextmanager.UpdateKey
	content string
}

func (session *Session) prepareTurn(ctx context.Context, services *SessionServices, goal string, turnContext *TurnContext) error {
	if session == nil || services == nil || turnContext == nil {
		return fmt.Errorf("turn context preparation is incomplete")
	}
	if err := session.refreshAgentsMd(ctx, services, turnContext.TurnID, turnContext.CWD); err != nil {
		return err
	}
	if err := services.prepareStaticTurnContext(ctx, turnContext, session.ContextUpdate, session.AppendItems); err != nil {
		return err
	}
	return services.prepareInputContext(ctx, goal, turnContext, session.ContextUpdate, session.AppendItems)
}

func (services *SessionServices) prepareStaticTurnContext(ctx context.Context, turnContext *TurnContext, contextUpdate func(contextmanager.UpdateKey) string, appendItems func(context.Context, protocol.TurnID, ...rollout.RolloutItem) error) error {
	if services == nil || turnContext == nil || contextUpdate == nil || appendItems == nil {
		return fmt.Errorf("static turn context preparation is incomplete")
	}
	include := func(spec tool.ToolSpec) bool {
		return turnContext.Mode != ModeKindPlan || planModeToolAllowed(spec)
	}
	tools := services.tools.SnapshotRouter(services.visibility, tool.RequestSnapshot{}, composeToolFilters(include, services.source)).Specs()
	toolNames := make([]string, len(tools))
	for index, spec := range tools {
		toolNames[index] = spec.Name
	}
	modelMessages, err := services.ModelMessages(services.ModelInfo())
	if err != nil {
		return err
	}
	var developer string
	if services.source.IsSubAgent() {
		developer, err = internalprompt.RenderSubagentDeveloperInstructions(modelMessages, toolNames)
	} else {
		developer, err = internalprompt.RenderCollaborationInstructions(modelMessages, turnContext.Mode, toolNames)
	}
	if err != nil {
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
	mcpContext := ""
	if services.mcp != nil {
		mcpContext = "MCP tools are available only through their exposed Tool Specs and current bindings."
	}
	environmentContext := fmt.Sprintf("<cwd>%s</cwd>", html.EscapeString(turnContext.CWD))
	if !services.source.IsSubAgent() && services.AgentControl != nil {
		if subagents := renderSubagents(services.AgentControl.SnapshotAll()); subagents != "" {
			environmentContext += "\n" + subagents
		}
	}
	return persistPreparedContextUpdates(ctx, turnContext.TurnID, contextUpdate, appendItems,
		preparedContextUpdate{key: contextmanager.UpdateCollaborationMode, content: developer},
		preparedContextUpdate{key: contextmanager.UpdateEnvironment, content: environmentContext},
		preparedContextUpdate{key: contextmanager.UpdatePermissionMode, content: "## Permission And Isolation Context\n\nPermission context (enforced by runtime, not by this text): " + string(encodedPermission)},
		preparedContextUpdate{key: contextmanager.UpdateMCP, content: mcpContext},
	)
}

func renderSubagents(records []multiagent.AgentRecord) string {
	if len(records) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("<subagents>")
	for _, record := range records {
		builder.WriteString("\n  - ")
		builder.WriteString(html.EscapeString(record.Metadata.ThreadID.String()))
		builder.WriteString(": ")
		builder.WriteString(html.EscapeString(record.Metadata.AgentNickname))
		builder.WriteString(" [")
		builder.WriteString(html.EscapeString(record.Metadata.AgentRole))
		builder.WriteString("] ")
		builder.WriteString(html.EscapeString(string(record.Status.Kind)))
	}
	builder.WriteString("\n</subagents>")
	return builder.String()
}

func (services *SessionServices) prepareInputContext(ctx context.Context, input string, turnContext *TurnContext, contextUpdate func(contextmanager.UpdateKey) string, appendItems func(context.Context, protocol.TurnID, ...rollout.RolloutItem) error) error {
	if services == nil || turnContext == nil || contextUpdate == nil || appendItems == nil {
		return fmt.Errorf("input-dependent context preparation is incomplete")
	}
	skillContext, err := services.renderInputSkillContext(input)
	if err != nil {
		return err
	}
	return persistPreparedContextUpdates(ctx, turnContext.TurnID, contextUpdate, appendItems,
		preparedContextUpdate{key: contextmanager.UpdateSkills, content: skillContext},
	)
}

func (services *SessionServices) renderInputSkillContext(input string) (string, error) {
	skillParts := make([]string, 0)
	if services.skills != nil {
		documents, err := services.skills.ResolveExplicit(input)
		if err != nil {
			return "", err
		}
		for _, document := range documents {
			encoded, encodeErr := json.Marshal(struct {
				Type     string `json:"type"`
				Name     string `json:"name"`
				Path     string `json:"path"`
				Revision string `json:"revision"`
			}{Type: "amadeus.skill_injection.v2", Name: document.Name, Path: document.PathToSkillMD, Revision: document.Revision})
			if encodeErr != nil {
				return "", encodeErr
			}
			skillParts = append(skillParts, string(encoded)+"\n"+strings.TrimSpace(document.Content))
		}
	}
	index := services.SkillIndex()
	indexParts := make([]string, 0, len(index))
	for _, entry := range index {
		if entry.Enabled {
			indexParts = append(indexParts, fmt.Sprintf("- `%s`: %s", entry.Name, entry.Description))
		}
	}
	if len(indexParts) > 0 {
		encoded, err := json.Marshal(struct {
			Type   string                `json:"type"`
			Skills []skill.SkillMetadata `json:"skills"`
		}{Type: "amadeus.skill_index.v2", Skills: index})
		if err != nil {
			return "", err
		}
		skillParts = append([]string{"## Skills\n\n" + string(encoded)}, skillParts...)
	} else if len(skillParts) > 0 {
		skillParts = append([]string{"## Skills"}, skillParts...)
	} else {
		skillParts = []string{"## Skills\n\nNo Skills are currently available."}
	}
	return strings.Join(skillParts, "\n\n"), nil
}

func persistPreparedContextUpdates(ctx context.Context, turnID protocol.TurnID, contextUpdate func(contextmanager.UpdateKey) string, appendItems func(context.Context, protocol.TurnID, ...rollout.RolloutItem) error, updates ...preparedContextUpdate) error {
	items := make([]rollout.RolloutItem, 0, len(updates))
	for _, update := range updates {
		worldState := contextmanager.NewWorldState()
		if err := worldState.Set(update.key, update.content); err != nil {
			return err
		}
		fragment := worldState.Fragment(update.key)
		rendered := fragment.Render()
		if contextUpdate(update.key) == rendered {
			continue
		}
		event := protocol.ContextUpdateEvent{Key: string(update.key)}
		if rendered != "" {
			event.Content = rendered
			event.Revision = fragment.Revision()
		}
		item, err := rollout.NewEventMsgItem(event)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil
	}
	if err := appendItems(ctx, turnID, items...); err != nil {
		return fmt.Errorf("persist context updates: %w", err)
	}
	return nil
}
