package builtin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type UpdatePlanOptions struct {
	Events protocol.EventSink
}

type UpdatePlan struct {
	options UpdatePlanOptions
}

func NewUpdatePlan(options UpdatePlanOptions) (*UpdatePlan, error) {
	if options.Events == nil {
		return nil, errors.New("update_plan event sink is nil")
	}
	return &UpdatePlan{options: options}, nil
}

func (updatePlan *UpdatePlan) Spec() tool.ToolSpec { return updatePlanSpec() }

func (updatePlan *UpdatePlan) SupportsParallelToolCalls() bool { return false }

func (updatePlan *UpdatePlan) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	var arguments protocol.UpdatePlanArgs
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return err
	}
	return arguments.Validate()
}

func (updatePlan *UpdatePlan) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var arguments protocol.UpdatePlanArgs
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return tool.PreparedToolUse{}, err
	}
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: arguments, Permission: tool.AllowPermission()}, nil
}

func (updatePlan *UpdatePlan) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	arguments, ok := prepared.State.(protocol.UpdatePlanArgs)
	if !ok {
		return tool.ToolResult{}, errors.New("update_plan preparation state is invalid")
	}
	invocation := prepared.Invocation
	turnID := protocol.TurnID(strings.TrimSpace(string(invocation.TurnID)))
	if turnID == "" {
		return tool.ToolResult{}, errors.New("update_plan turn ID is empty")
	}
	if err := updatePlan.options.Events.Publish(toolContext.Context, protocol.Event{Msg: protocol.PlanUpdateEvent{
		TurnID: protocol.TurnID(turnID), UpdatePlanArgs: arguments,
	}}); err != nil {
		return tool.ToolResult{}, fmt.Errorf("publish plan update: %w", err)
	}
	return tool.ToolResult{ToolName: "update_plan", Text: "Plan updated"}, nil
}

func updatePlanSpec() tool.ToolSpec {
	return tool.ToolSpec{
		Name: "update_plan", Description: "Create or replace the visible execution checklist for a complex task. Keep exactly one item in_progress while work remains; this is soft guidance and does not schedule tools.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"explanation":{"type":"string"},"plan":{"type":"array","items":{"type":"object","properties":{"step":{"type":"string","minLength":1},"status":{"type":"string","enum":["pending","in_progress","completed"]}},"required":["step","status"],"additionalProperties":false}}},"required":["plan"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectNone, Idempotent: false,
	}
}

var _ tool.ToolDefinition = (*UpdatePlan)(nil)
