package session

import (
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func (services *SessionServices) CaptureStep(snapshot func(llm.ModelInfo, llm.Prompt) agentcontext.PromptSnapshot, turnContext turn.TurnContext) (engine.StepContext, error) {
	if services == nil || snapshot == nil {
		return engine.StepContext{}, errors.New("step context capture is incomplete")
	}
	tools := services.AvailableTools()
	if turnContext.Mode == turn.ModeKindPlan {
		tools = engine.PlanModeTools(tools)
	}
	toolNames := make([]string, len(tools))
	definitions := make([]llm.ToolSpec, len(tools))
	toolSpecRevisions := make([]string, len(tools))
	for index, spec := range tools {
		toolNames[index] = spec.Name
		definitions[index] = llm.ToolSpec{Name: spec.Name, Description: spec.Description, InputSchema: append([]byte(nil), spec.InputSchema...)}
		toolSpecRevisions[index] = definitions[index].RevisionID()
	}
	model := services.ModelInfo()
	modelMessages, err := services.ModelMessages(model)
	if err != nil {
		return engine.StepContext{}, err
	}
	baseInstructions, err := modelMessages.ResolveBaseInstructions(string(turnContext.Personality))
	if err != nil {
		return engine.StepContext{}, err
	}
	promptShape := llm.Prompt{
		BaseInstructions: baseInstructions, Tools: definitions,
		ParallelToolCalls: model.SupportsParallelToolCalls,
		OutputSchema:      append(llm.OutputSchema(nil), turnContext.OutputSchema...), OutputSchemaStrict: turnContext.OutputSchemaStrict,
	}
	promptSnapshot := snapshot(model, promptShape)
	requestSnapshot := tool.RequestSnapshot{ToolRevision: services.tools.Revision()}
	if services.skills != nil {
		requestSnapshot.SkillRevision, _ = services.skills.Revision()
	}
	if services.mcp != nil {
		requestSnapshot.MCPBindingRevision = services.mcp.Binding().Revision
	}
	return engine.StepContext{
		Turn: turnContext, Prompt: promptSnapshot, Model: model, BaseInstructions: baseInstructions,
		Tools: cloneSessionToolSpecs(tools), ToolNames: append([]string(nil), toolNames...),
		ToolSpecRevisions: append([]string(nil), toolSpecRevisions...),
		ToolRevision:      requestSnapshot.ToolRevision, SkillRevision: requestSnapshot.SkillRevision,
		MCPRevision: requestSnapshot.MCPBindingRevision, ModelMessagesRevision: modelMessages.Revision,
		WorldStateRevision: promptSnapshot.WorldStateRevision, RequestSnapshot: requestSnapshot,
	}, nil
}

func cloneSessionToolSpecs(specs []tool.ToolSpec) []tool.ToolSpec {
	result := make([]tool.ToolSpec, len(specs))
	for index, spec := range specs {
		result[index] = spec.Clone()
	}
	return result
}
