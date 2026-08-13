package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type PlanUpdater interface {
	UpdatePlan(context.Context, turn.ID, plan.Update) (plan.Snapshot, error)
}

type UpdatePlanOptions struct {
	Events event.Sink
}

type UpdatePlan struct {
	updater PlanUpdater
	options UpdatePlanOptions
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

func (updatePlan *UpdatePlan) Call(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	call := invocation.Call
	var update plan.Update
	if err := decodeArguments(call.Payload, &update); err != nil {
		return tool.Output{}, err
	}
	turnID := turn.ID(strings.TrimSpace(invocation.TurnID))
	if turnID == "" {
		return tool.Output{}, errors.New("update_plan turn ID is empty")
	}
	snapshot, err := updatePlan.updater.UpdatePlan(ctx, turnID, update)
	if err != nil {
		return tool.Output{}, fmt.Errorf("update session plan: %w", err)
	}
	items := make([]event.PlanItem, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		items = append(items, event.PlanItem{Step: item.Step, Status: string(item.Status)})
	}
	if err := updatePlan.options.Events.Publish(ctx, event.PlanUpdated{
		Explanation: snapshot.Explanation, Items: items, Revision: snapshot.Revision, UpdatedAt: snapshot.UpdatedAt,
	}); err != nil {
		return tool.Output{}, fmt.Errorf("publish plan update: %w", err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return tool.Output{}, fmt.Errorf("encode plan update result: %w", err)
	}
	return tool.Output{ToolName: "update_plan", Text: string(encoded), Metadata: map[string]any{
		"revision": snapshot.Revision, "items": len(snapshot.Items), "explanation": strings.TrimSpace(snapshot.Explanation),
	}}, nil
}

func updatePlanSpec() tool.ToolSpec {
	return tool.ToolSpec{
		Name: "update_plan", Description: "Create or replace the visible execution checklist for a complex task. Keep exactly one item in_progress while work remains; this is soft guidance and does not schedule tools.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"explanation":{"type":"string"},"items":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"object","properties":{"step":{"type":"string","minLength":1},"status":{"type":"string","enum":["pending","in_progress","completed"]}},"required":["step","status"],"additionalProperties":false}}},"required":["items"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectNone, Idempotent: false,
	}
}

var _ tool.Tool = (*UpdatePlan)(nil)
