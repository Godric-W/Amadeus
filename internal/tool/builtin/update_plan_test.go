package builtin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/agent/plan"
)

func TestUpdatePlanAppliesRecordsAndPublishes(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	events := event.NewMemorySink()
	var recorded plan.Snapshot
	candidate, err := NewUpdatePlan(plan.NewState(), UpdatePlanOptions{
		Now: func() time.Time { return now }, Events: events,
		Recorder: func(_ context.Context, snapshot plan.Snapshot) error { recorded = snapshot; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executePreparedTool(t, context.Background(), candidate, json.RawMessage(`{"explanation":"implementation","items":[{"step":"Inspect","status":"completed"},{"step":"Patch","status":"in_progress"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Revision != 1 || len(recorded.Items) != 2 || recorded.Items[1].Status != plan.ItemInProgress {
		t.Fatalf("unexpected recorded plan: %#v", recorded)
	}
	if result.ToolName != "update_plan" || result.Metadata["revision"] != int64(1) {
		t.Fatalf("unexpected Tool result: %#v", result)
	}
	published := events.Snapshot()
	if len(published) != 1 {
		t.Fatalf("unexpected events: %#v", published)
	}
	updated, ok := published[0].(event.PlanUpdated)
	if !ok || updated.Revision != 1 || len(updated.Items) != 2 || updated.Items[1].Status != "in_progress" || updated.Items[1].Step != "Patch" {
		t.Fatalf("unexpected PlanUpdated event: %#v", published[0])
	}
}

func TestUpdatePlanRejectsMultipleInProgressItems(t *testing.T) {
	candidate, err := NewUpdatePlan(plan.NewState(), UpdatePlanOptions{
		Events: event.NewMemorySink(), Recorder: func(context.Context, plan.Snapshot) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = executePreparedTool(t, context.Background(), candidate, json.RawMessage(`{"items":[{"step":"A","status":"in_progress"},{"step":"B","status":"in_progress"}]}`))
	if err == nil {
		t.Fatal("multiple in-progress items were accepted")
	}
}
