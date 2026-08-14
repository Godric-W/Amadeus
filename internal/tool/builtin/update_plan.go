package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type PlanUpdater interface {
	UpdatePlan(context.Context, turn.ID, plan.Update) (plan.Snapshot, error)
}

type UpdatePlanOptions struct {
	Events protocol.EventSink
}

type UpdatePlan struct {
	updater PlanUpdater
	options UpdatePlanOptions
}

type updatePlanArguments struct {
	Explanation string      `json:"explanation,omitempty"`
	Plan        []plan.Item `json:"plan"`
}

func NewUpdatePlan(updater PlanUpdater, options UpdatePlanOptions) (*UpdatePlan, error) {
	if updater == nil {
		return nil, errors.New("update_plan updater is nil")
	}
	if options.Events == nil {
		return nil, errors.New("update_plan event sink is nil")
	}
	return &UpdatePlan{updater: updater, options: options}, nil
}

func (updatePlan *UpdatePlan) Spec() tool.ToolSpec { return updatePlanSpec() }

func (updatePlan *UpdatePlan) SupportsParallelToolCalls() bool { return false }

func (updatePlan *UpdatePlan) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	var arguments updatePlanArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return err
	}
	_, err := plan.NewState().Apply(plan.Update{Explanation: arguments.Explanation, Items: arguments.Plan}, time.Time{})
	return err
}

func (updatePlan *UpdatePlan) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var arguments updatePlanArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return tool.PreparedToolUse{}, err
	}
	update := plan.Update{Explanation: arguments.Explanation, Items: arguments.Plan}
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: update, Permission: tool.AllowPermission()}, nil
}

func (updatePlan *UpdatePlan) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	update, ok := prepared.State.(plan.Update)
	if !ok {
		return tool.ToolResult{}, errors.New("update_plan preparation state is invalid")
	}
	invocation := prepared.Invocation
	turnID := turn.ID(strings.TrimSpace(invocation.TurnID))
	if turnID == "" {
		return tool.ToolResult{}, errors.New("update_plan turn ID is empty")
	}
	snapshot, err := updatePlan.updater.UpdatePlan(toolContext.Context, turnID, update)
	if err != nil {
		return tool.ToolResult{}, fmt.Errorf("update session plan: %w", err)
	}
	items := make([]protocol.PlanItem, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		items = append(items, protocol.PlanItem{Step: item.Step, Status: string(item.Status)})
	}
	if err := updatePlan.options.Events.Publish(toolContext.Context, protocol.SessionEvent{Message: protocol.PlanUpdated{
		Explanation: snapshot.Explanation, Items: items, Revision: snapshot.Revision, UpdatedAt: snapshot.UpdatedAt,
	}}); err != nil {
		return tool.ToolResult{}, fmt.Errorf("publish plan update: %w", err)
	}
	return tool.ToolResult{ToolName: "update_plan", Text: "Plan updated", Data: snapshot, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: "Plan updated"}, Metadata: map[string]any{
		"revision": snapshot.Revision, "items": len(snapshot.Items), "explanation": strings.TrimSpace(snapshot.Explanation),
	}}, nil
}

func updatePlanSpec() tool.ToolSpec {
	return tool.ToolSpec{
		Name: "update_plan", Description: "Create or replace the visible execution checklist for a complex task. Keep exactly one item in_progress while work remains; this is soft guidance and does not schedule tools.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"explanation":{"type":"string"},"plan":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"object","properties":{"step":{"type":"string","minLength":1},"status":{"type":"string","enum":["pending","in_progress","completed"]}},"required":["step","status"],"additionalProperties":false}}},"required":["plan"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectNone, Idempotent: false,
	}
}

var _ tool.ToolDefinition = (*UpdatePlan)(nil)
