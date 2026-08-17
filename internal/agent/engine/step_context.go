package engine

import (
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type StepInstructionScope interface {
	tool.ContextScope
	MarkSampled()
}

// StepContext is an immutable request snapshot. The prompt-visible tool specs
// and the execution allow-list are derived together and must stay paired.
type StepContext struct {
	Turn             turn.TurnContext
	Prompt           agentcontext.PromptSnapshot
	Model            llm.ModelInfo
	BaseInstructions llm.BaseInstructions
	Tools            []tool.ToolSpec
	ToolNames        []string
	ToolRevision     string
	SkillRevision    string
	MCPRevision      string
	RequestSnapshot  tool.RequestSnapshot
}

func (runtime *Services) CaptureStep(snapshot func(llm.ModelInfo, llm.Prompt) agentcontext.PromptSnapshot, turnContext turn.TurnContext) (StepContext, error) {
	if runtime == nil || snapshot == nil {
		return StepContext{}, errors.New("step context capture is incomplete")
	}
	tools := runtime.AvailableTools()
	if turnContext.Mode == turn.ModeKindPlan {
		tools = planModeTools(tools)
	}
	toolNames := make([]string, len(tools))
	definitions := make([]llm.ToolDefinition, len(tools))
	for index, spec := range tools {
		toolNames[index] = spec.Name
		definitions[index] = llm.ToolDefinition{Name: spec.Name, Description: spec.Description, InputSchema: append([]byte(nil), spec.InputSchema...)}
	}
	model := runtime.ModelInfo()
	promptShape := llm.Prompt{
		BaseInstructions: runtime.baseInstructions, Tools: definitions,
		ParallelToolCalls: model.SupportsParallelToolCalls,
		OutputSchema:      append(llm.OutputSchema(nil), turnContext.OutputSchema...),
	}
	promptSnapshot := snapshot(model, promptShape)
	requestSnapshot := tool.RequestSnapshot{ToolRevision: runtime.registry.Revision()}
	if runtime.extensions != nil {
		requestSnapshot.SkillRevision = runtime.extensions.SkillRevision()
		requestSnapshot.MCPBindingRevision = runtime.extensions.MCPBinding().Revision
	}
	return StepContext{
		Turn: turnContext, Prompt: promptSnapshot, Model: model, BaseInstructions: runtime.baseInstructions,
		Tools: cloneToolSpecs(tools), ToolNames: append([]string(nil), toolNames...),
		ToolRevision: requestSnapshot.ToolRevision, SkillRevision: requestSnapshot.SkillRevision,
		MCPRevision: requestSnapshot.MCPBindingRevision, RequestSnapshot: requestSnapshot,
	}, nil
}

func planModeTools(specs []tool.ToolSpec) []tool.ToolSpec {
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

func cloneToolSpecs(specs []tool.ToolSpec) []tool.ToolSpec {
	result := make([]tool.ToolSpec, len(specs))
	for index, spec := range specs {
		result[index] = spec.Clone()
	}
	return result
}
