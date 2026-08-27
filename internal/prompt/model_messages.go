package prompt

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/prompt/builtin"
)

func LoadModelMessages() (llm.ModelMessages, error) {
	if err := builtin.Validate(); err != nil {
		return llm.ModelMessages{}, err
	}
	base, err := joinBuiltin(builtin.AgentSystemLayers())
	if err != nil {
		return llm.ModelMessages{}, err
	}
	defaultInstructions, err := joinBuiltin([]builtin.ID{builtin.ModeExecute})
	if err != nil {
		return llm.ModelMessages{}, err
	}
	planInstructions, err := joinBuiltin([]builtin.ID{builtin.ModePlan})
	if err != nil {
		return llm.ModelMessages{}, err
	}
	subagent, err := builtin.Read(builtin.AgentSubagent)
	if err != nil {
		return llm.ModelMessages{}, err
	}
	return llm.ModelMessages{
		InstructionsTemplate: base,
		CollaborationModes: llm.CollaborationModeMessages{
			Default: defaultInstructions,
			Plan:    planInstructions,
		},
		MultiAgent:            llm.MultiAgentMessages{Role: llm.MultiAgentRoleMessages{Subagent: strings.TrimSpace(subagent)}},
		InstructionsRevision:  builtin.RevisionFor(builtin.AgentSystemLayers()),
		CollaborationRevision: builtin.RevisionFor([]builtin.ID{builtin.ModeExecute, builtin.ModePlan}),
		MultiAgentRevision:    builtin.RevisionFor([]builtin.ID{builtin.AgentSubagent}),
		Source:                "amadeus.builtin",
	}, nil
}

func joinBuiltin(ids []builtin.ID) (string, error) {
	if len(ids) == 0 {
		return "", errors.New("Prompt asset list is empty")
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		content, err := builtin.Read(id)
		if err != nil {
			return "", fmt.Errorf("load Prompt asset %q: %w", id, err)
		}
		parts = append(parts, strings.TrimSpace(content))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n")), nil
}
