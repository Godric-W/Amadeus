package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/multiagent"
	"github.com/Godric-W/Amadeus/internal/contextmanager"
	"github.com/Godric-W/Amadeus/internal/llm"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func (session *Session) syncWorldState(ctx context.Context, services *SessionServices, step StepContext) error {
	if session == nil || services == nil {
		return errors.New("world state sync is incomplete")
	}
	state, err := buildStepWorldState(services, step)
	if err != nil {
		return err
	}
	previous, known := session.state.Context.WorldStateBaseline()
	baselineKind := session.state.Context.WorldStateBaselineKind()
	fragments, current, err := state.Render(previous, baselineKind)
	if err != nil {
		return err
	}
	sections := current
	if known {
		sections = contextmanager.WorldStatePatch(previous, current)
	}
	if len(fragments) == 0 && len(sections) == 0 {
		return nil
	}
	messages, err := mergeContextFragments(fragments)
	if err != nil {
		return err
	}
	items := make([]rollout.RolloutItem, 0, len(messages)+1)
	for _, message := range messages {
		item, err := rollout.NewContextResponseItem(message, rollout.ContextKindWorldState)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	items = append(items, rollout.WorldStateItem{Full: baselineKind != contextmanager.PreviousSectionKnown, Sections: sections})
	if err := session.AppendItems(ctx, step.Turn.TurnID, items...); err != nil {
		return fmt.Errorf("persist world state: %w", err)
	}
	return nil
}

func buildStepWorldState(services *SessionServices, step StepContext) (*contextmanager.WorldState, error) {
	turnContext := step.Turn
	messages, err := services.ModelMessages(step.Model)
	if err != nil {
		return nil, err
	}
	mode, err := internalprompt.RenderCollaborationInstructions(messages, turnContext.Mode)
	if err != nil {
		return nil, err
	}
	state := contextmanager.NewWorldState()
	add := func(options contextmanager.TextSectionOptions) error {
		section, err := contextmanager.NewTextSection(options)
		if err != nil {
			return err
		}
		return state.Add(section)
	}
	modelSection, err := contextmanager.NewSnapshotSection("model", struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}{Provider: step.Model.Provider, Model: step.Model.Name})
	if err != nil {
		return nil, err
	}
	if err := state.Add(modelSection); err != nil {
		return nil, err
	}
	if turnContext.Personality != "" {
		personalitySection, err := contextmanager.NewSnapshotSection("personality", struct {
			Value Personality `json:"value"`
		}{Value: turnContext.Personality})
		if err != nil {
			return nil, err
		}
		if err := state.Add(personalitySection); err != nil {
			return nil, err
		}
	}
	if services.source.IsSubAgent() {
		role, err := internalprompt.RenderSubagentRoleInstructions(messages)
		if err != nil {
			return nil, err
		}
		if err := add(contextmanager.TextSectionOptions{ID: "multi_agent_role", Kind: "multi_agent.role_instructions", Role: llm.RoleDeveloper, Text: role, OpenMarker: "<multi_agent_role>", CloseMarker: "</multi_agent_role>", Separate: true}); err != nil {
			return nil, err
		}
	}
	if err := add(contextmanager.TextSectionOptions{ID: "collaboration_mode", Kind: "collaboration_mode.instructions", Role: llm.RoleDeveloper, Text: mode, OpenMarker: "<collaboration_mode>", CloseMarker: "</collaboration_mode>"}); err != nil {
		return nil, err
	}
	agents := step.LoadedAgentsMd.Render()
	if err := add(contextmanager.TextSectionOptions{
		ID: "agents_md", Kind: "agents_md.instructions", Role: llm.RoleUser, Text: agents,
		ReplacementNotice: "These AGENTS.md instructions replace all previously provided AGENTS.md instructions.",
		RemovalNotice:     "The previously provided AGENTS.md instructions no longer apply.",
	}); err != nil {
		return nil, err
	}
	if err := add(contextmanager.TextSectionOptions{ID: "environment", Kind: "environment.context", Role: llm.RoleUser, Text: renderEnvironmentContext(step), OpenMarker: "<environment_context>", CloseMarker: "</environment_context>"}); err != nil {
		return nil, err
	}
	permissions, err := renderPermissionContext(step, messages)
	if err != nil {
		return nil, err
	}
	if err := add(contextmanager.TextSectionOptions{ID: "permissions", Kind: "permissions.instructions", Role: llm.RoleDeveloper, Text: permissions, OpenMarker: "<permission_context>", CloseMarker: "</permission_context>"}); err != nil {
		return nil, err
	}
	if err := add(contextmanager.TextSectionOptions{ID: "skills_catalog", Kind: "skills.catalog", Role: llm.RoleDeveloper, Text: renderSkillCatalog(step.Skills), OpenMarker: "<skills_instructions>", CloseMarker: "</skills_instructions>"}); err != nil {
		return nil, err
	}
	return state, nil
}

func renderEnvironmentContext(step StepContext) string {
	turnContext := step.Turn
	var builder strings.Builder
	builder.WriteString("<cwd>")
	builder.WriteString(html.EscapeString(turnContext.CWD))
	builder.WriteString("</cwd>")
	if turnContext.Shell != "" {
		builder.WriteString("\n<shell>")
		builder.WriteString(html.EscapeString(turnContext.Shell))
		builder.WriteString("</shell>")
	}
	if turnContext.CurrentDate != "" {
		builder.WriteString("\n<current_date>")
		builder.WriteString(html.EscapeString(turnContext.CurrentDate))
		builder.WriteString("</current_date>")
	}
	if turnContext.Timezone != "" {
		builder.WriteString("\n<timezone>")
		builder.WriteString(html.EscapeString(turnContext.Timezone))
		builder.WriteString("</timezone>")
	}
	builder.WriteString(renderSubagents(step.Subagents))
	return builder.String()
}

func renderPermissionContext(step StepContext, messages llm.ModelMessages) (string, error) {
	guidance := make([]string, 0, 3)
	if value := strings.TrimSpace(messages.Permissions.WorkspaceWrite); value != "" {
		guidance = append(guidance, value)
	}
	if value := strings.TrimSpace(messages.Approvals.OnRequest); value != "" {
		guidance = append(guidance, value)
	}
	if len(step.PermissionProfile.WorkspaceRoots) == 0 && len(step.PermissionProfile.TemporaryRoots) == 0 && len(step.PermissionProfile.ReadOnlyRoots) == 0 && len(step.PermissionProfile.DeniedRoots) == 0 && !step.PermissionProfile.ReadHost {
		guidance = append(guidance, "Permission context is enforced by the runtime; no filesystem profile is available for this session.")
		return strings.Join(guidance, "\n\n"), nil
	}
	effective := step.PermissionProfile
	payload := struct {
		ReadHost       bool     `json:"read_host"`
		WorkspaceRoots []string `json:"workspace_roots"`
		TemporaryRoots []string `json:"temporary_roots"`
		ReadOnlyRoots  []string `json:"read_only_roots"`
		DeniedRoots    []string `json:"denied_roots"`
		ApprovalCount  int      `json:"session_command_approval_count"`
	}{
		ReadHost: effective.ReadHost, WorkspaceRoots: effective.WorkspaceRoots,
		TemporaryRoots: effective.TemporaryRoots, ReadOnlyRoots: effective.ReadOnlyRoots,
		DeniedRoots: effective.DeniedRoots, ApprovalCount: step.PermissionGrants,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	guidance = append(guidance, "Permission context (enforced by runtime, not by this text): "+string(encoded))
	return strings.Join(guidance, "\n\n"), nil
}

func renderSubagents(records []multiagent.AgentRecord) string {
	if len(records) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("\n<subagents>")
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

func mergeContextFragments(fragments []contextmanager.ContextFragment) ([]llm.ResponseItem, error) {
	result := make([]llm.ResponseItem, 0, len(fragments))
	for _, fragment := range fragments {
		message, err := fragment.ResponseItem()
		if err != nil {
			return nil, err
		}
		last := len(result) - 1
		if last >= 0 && !fragment.Separate && result[last].Role == message.Role {
			result[last].Content += "\n\n" + message.Content
			continue
		}
		result = append(result, message)
	}
	return result, nil
}
