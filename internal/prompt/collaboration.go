package prompt

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/prompt/builtin"
)

func RenderCollaborationInstructions(messages llm.ModelMessages, mode turn.ModeKind, toolNames []string) (string, error) {
	var content string
	switch mode {
	case turn.ModeKindDefault:
		content = messages.CollaborationModes.Default
	case turn.ModeKindPlan:
		content = messages.CollaborationModes.Plan
	default:
		return "", fmt.Errorf("unsupported collaboration mode %q", mode)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", errors.New("collaboration mode instructions are empty")
	}
	guidance, err := toolGuidance(toolNames)
	if err != nil {
		return "", err
	}
	if guidance == "" {
		return content, nil
	}
	return content + "\n\n" + guidance, nil
}

func toolGuidance(toolNames []string) (string, error) {
	visible := make(map[string]struct{}, len(toolNames))
	for _, name := range toolNames {
		name = strings.TrimSpace(name)
		if name == "" {
			return "", errors.New("visible Tool name is empty")
		}
		visible[name] = struct{}{}
	}
	ids := make([]builtin.ID, 0, len(visible))
	for _, entry := range builtin.ToolPromptOrder() {
		if _, ok := visible[entry.Name]; ok {
			ids = append(ids, entry.Prompt)
		}
	}
	if len(ids) == 0 {
		return "", nil
	}
	return joinBuiltin(ids)
}
