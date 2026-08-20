package engine

import (
	"context"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/instruction"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type StepInstructionScope interface {
	tool.ContextScope
	MarkSampled()
}

type InstructionScope interface {
	StepInstructionScope
	Initialize(context.Context, string) (instruction.ResolveRequest, error)
}

type StepContext struct {
	Turn                  turn.TurnContext
	Prompt                agentcontext.PromptSnapshot
	Model                 llm.ModelInfo
	BaseInstructions      llm.BaseInstructions
	Tools                 []tool.ToolSpec
	ToolNames             []string
	ToolSpecRevisions     []string
	ToolRevision          string
	SkillRevision         string
	MCPRevision           string
	ModelMessagesRevision string
	WorldStateRevision    string
	RequestSnapshot       tool.RequestSnapshot
}

func PlanModeTools(specs []tool.ToolSpec) []tool.ToolSpec {
	allowedNetwork := map[string]struct{}{"web_search": {}, "web_fetch": {}, "mcp_list_tools": {}, "mcp_list_resources": {}, "mcp_read_resource": {}}
	result := make([]tool.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.Name == "update_plan" {
			continue
		}
		if spec.SideEffect == tool.SideEffectNone || spec.SideEffect == tool.SideEffectRead {
			result = append(result, spec.Clone())
			continue
		}
		if _, ok := allowedNetwork[strings.TrimSpace(spec.Name)]; ok {
			result = append(result, spec.Clone())
		}
	}
	return result
}
