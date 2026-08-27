package prompt

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
)

func RenderCollaborationInstructions(messages llm.ModelMessages, mode protocol.ModeKind) (string, error) {
	var content string
	switch mode {
	case protocol.ModeKindDefault:
		content = messages.CollaborationModes.Default
	case protocol.ModeKindPlan:
		content = messages.CollaborationModes.Plan
	default:
		return "", fmt.Errorf("unsupported collaboration mode %q", mode)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", errors.New("collaboration mode instructions are empty")
	}
	return content, nil
}

func RenderSubagentRoleInstructions(messages llm.ModelMessages) (string, error) {
	content := strings.TrimSpace(messages.MultiAgent.Role.Subagent)
	if content == "" {
		return "", errors.New("sub-agent developer instructions are empty")
	}
	return content, nil
}
