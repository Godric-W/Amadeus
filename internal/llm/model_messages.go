package llm

import (
	"errors"
	"strings"
)

type ModelMessages struct {
	InstructionsTemplate  string                     `json:"instructions_template,omitempty"`
	InstructionsVariables ModelInstructionsVariables `json:"instructions_variables,omitempty"`
	Approvals             ApprovalMessages           `json:"approvals,omitempty"`
	Permissions           PermissionMessages         `json:"permissions,omitempty"`
	CollaborationModes    CollaborationModeMessages  `json:"collaboration_modes,omitempty"`
	MultiAgent            MultiAgentMessages         `json:"multi_agent,omitempty"`

	InstructionsRevision  string `json:"instructions_revision,omitempty"`
	CollaborationRevision string `json:"collaboration_revision,omitempty"`
	MultiAgentRevision    string `json:"multi_agent_revision,omitempty"`
	Source                string `json:"source,omitempty"`
}

type ModelInstructionsVariables struct {
	PersonalityDefault   string `json:"personality_default,omitempty"`
	PersonalityFriendly  string `json:"personality_friendly,omitempty"`
	PersonalityPragmatic string `json:"personality_pragmatic,omitempty"`
}

type CollaborationModeMessages struct {
	Default string `json:"default,omitempty"`
	Plan    string `json:"plan,omitempty"`
}

type ApprovalMessages struct {
	OnRequest string `json:"on_request,omitempty"`
}

type PermissionMessages struct {
	WorkspaceWrite string `json:"workspace_write,omitempty"`
}

type MultiAgentMessages struct {
	Role MultiAgentRoleMessages `json:"role,omitempty"`
}

type MultiAgentRoleMessages struct {
	Subagent string `json:"subagent,omitempty"`
}

func (messages ModelMessages) ResolveBaseInstructions(personality, model string) (BaseInstructions, error) {
	template := strings.TrimSpace(messages.InstructionsTemplate)
	if template == "" {
		return BaseInstructions{}, errors.New("model instructions template is empty")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return BaseInstructions{}, errors.New("model instructions provenance model is empty")
	}
	personalityText := messages.InstructionsVariables.PersonalityDefault
	switch strings.TrimSpace(personality) {
	case "":
	case "friendly":
		personalityText = messages.InstructionsVariables.PersonalityFriendly
	case "pragmatic":
		personalityText = messages.InstructionsVariables.PersonalityPragmatic
	default:
		return BaseInstructions{}, errors.New("unsupported model personality")
	}
	return NewModelBaseInstructions(strings.ReplaceAll(template, "{{ personality }}", personalityText), model), nil
}

func (messages ModelMessages) HasInstructions() bool {
	return strings.TrimSpace(messages.InstructionsTemplate) != ""
}

func (messages ModelMessages) Normalized() ModelMessages {
	if strings.TrimSpace(messages.Source) == "" {
		messages.Source = "model"
	}
	return messages
}
