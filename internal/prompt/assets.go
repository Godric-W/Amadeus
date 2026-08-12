package prompt

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/prompt/builtin"
)

// Assets is the small, immutable built-in prompt surface used by the runtime.
// It deliberately has no provenance, hash or variable dependency graph.
type Assets struct {
	Base       llm.BaseInstructions
	Compaction llm.BaseInstructions
}

func LoadAssets() (Assets, error) {
	if err := builtin.Validate(); err != nil {
		return Assets{}, err
	}
	base, err := joinBuiltin(builtin.AgentSystemLayers())
	if err != nil {
		return Assets{}, err
	}
	compaction, err := builtin.Read(builtin.ContextCompaction)
	if err != nil {
		return Assets{}, err
	}
	return Assets{Base: llm.BaseInstructions{Text: base}, Compaction: llm.BaseInstructions{Text: compaction}}, nil
}

func DeveloperInstructions(mode string, toolNames []string) (string, error) {
	mode = strings.TrimSpace(mode)
	var modeLayer builtin.ID
	switch mode {
	case "execute":
		modeLayer = builtin.ModeExecute
	case "plan":
		modeLayer = builtin.ModePlan
	default:
		return "", fmt.Errorf("unknown Prompt run mode %q", mode)
	}
	layers := []builtin.ID{modeLayer}
	if mode == "execute" {
		layers = append(layers, builtin.ToolsGeneral)
		visible := make(map[string]bool, len(toolNames))
		for _, name := range toolNames {
			visible[strings.TrimSpace(name)] = true
		}
		if visible["update_plan"] {
			layers = append(layers, builtin.ToolUpdatePlan)
		}
		if visible["apply_patch"] {
			layers = append(layers, builtin.ToolApplyPatch)
		}
		if visible["execute_command"] {
			layers = append(layers, builtin.ToolExecuteCommand)
		}
	}
	return joinBuiltin(layers)
}

func joinBuiltin(ids []builtin.ID) (string, error) {
	if len(ids) == 0 {
		return "", errors.New("prompt asset list is empty")
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		content, err := builtin.Read(id)
		if err != nil {
			return "", fmt.Errorf("load prompt asset %q: %w", id, err)
		}
		parts = append(parts, strings.TrimSpace(content))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n")), nil
}
