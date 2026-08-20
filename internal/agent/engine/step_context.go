package engine

import (
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type StepContext struct {
	Turn                  turn.TurnContext
	Prompt                agentcontext.PromptSnapshot
	Model                 llm.ModelInfo
	BaseInstructions      llm.BaseInstructions
	ToolRouter            tool.ToolRouter
	ModelMessagesRevision string
	WorldStateRevision    string
}

func PlanModeTools(specs []tool.ToolSpec) []tool.ToolSpec {
	result := make([]tool.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		if PlanModeToolAllowed(spec) {
			result = append(result, spec.Clone())
		}
	}
	return result
}

func PlanModeToolAllowed(spec tool.ToolSpec) bool {
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
