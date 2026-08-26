package session

import (
	"errors"
	"strings"

	contextmanager "github.com/Godric-W/Amadeus/internal/contextmanager"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type StepContext struct {
	Turn                  TurnContext
	Prompt                contextmanager.PromptSnapshot
	Model                 llm.ModelInfo
	BaseInstructions      llm.BaseInstructions
	ToolRouter            tool.ToolRouter
	ModelMessagesRevision string
	WorldStateRevision    string
}

func (services *SessionServices) CaptureStep(snapshot func(llm.ModelInfo, llm.Prompt) contextmanager.PromptSnapshot, turnContext TurnContext) (StepContext, error) {
	if services == nil || snapshot == nil {
		return StepContext{}, errors.New("step context capture is incomplete")
	}
	requestSnapshot := tool.RequestSnapshot{}
	if services.skills != nil {
		requestSnapshot.SkillRevision, _ = services.skills.Revision()
	}
	if services.mcp != nil {
		requestSnapshot.MCPBindingRevision = services.mcp.Binding().Revision
	}
	if services.agentsMd != nil {
		requestSnapshot.AgentsMdRevision = services.agentsMd.Current().Revision
	}
	var include tool.ToolRouteFilter
	if turnContext.Mode == ModeKindPlan {
		include = planModeToolAllowed
	}
	include = composeToolFilters(include, services.source)
	router := services.tools.SnapshotRouter(services.visibility, requestSnapshot, include)
	tools := router.Specs()
	definitions := make([]llm.ToolSpec, len(tools))
	for index, spec := range tools {
		definitions[index] = llm.ToolSpec{Name: spec.Name, Description: spec.Description, InputSchema: append([]byte(nil), spec.InputSchema...)}
	}
	model := services.ModelInfo()
	modelMessages, err := services.ModelMessages(model)
	if err != nil {
		return StepContext{}, err
	}
	baseInstructions, err := modelMessages.ResolveBaseInstructions(string(turnContext.Personality))
	if err != nil {
		return StepContext{}, err
	}
	promptShape := llm.Prompt{
		BaseInstructions: baseInstructions, Tools: definitions,
		ParallelToolCalls: model.SupportsParallelToolCalls,
		OutputSchema:      append(llm.OutputSchema(nil), turnContext.OutputSchema...), OutputSchemaStrict: turnContext.OutputSchemaStrict,
	}
	promptSnapshot := snapshot(model, promptShape)
	return StepContext{
		Turn: turnContext, Prompt: promptSnapshot, Model: model, BaseInstructions: baseInstructions,
		ToolRouter: router, ModelMessagesRevision: modelMessages.Revision,
		WorldStateRevision: promptSnapshot.WorldStateRevision,
	}, nil
}

func planModeToolAllowed(spec tool.ToolSpec) bool {
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

func composeToolFilters(existing tool.ToolRouteFilter, source protocol.SessionSource) tool.ToolRouteFilter {
	if !source.IsSubAgent() {
		return existing
	}
	return func(spec tool.ToolSpec) bool {
		if existing != nil && !existing(spec) {
			return false
		}
		switch spec.Name {
		case "read", "glob", "grep", "read_skill", "web_search":
			return true
		default:
			return false
		}
	}
}
