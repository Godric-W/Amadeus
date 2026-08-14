package builtin

import (
	"context"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type testPlanUpdater struct {
	snapshot plan.Snapshot
	calls    int
}

func (updater *testPlanUpdater) UpdatePlan(_ context.Context, _ turn.ID, update plan.Update) (plan.Snapshot, error) {
	state := plan.NewState()
	if updater.snapshot.Revision > 0 {
		if err := state.Restore(updater.snapshot); err != nil {
			return plan.Snapshot{}, err
		}
	}
	updater.calls++
	snapshot, err := state.Apply(update, time.Date(2026, 8, 5, 12, 0, updater.calls, 0, time.UTC))
	if err == nil {
		updater.snapshot = snapshot
	}
	return snapshot, err
}

func TestUpdatePlanAppliesAndPublishes(t *testing.T) {
	updater := &testPlanUpdater{}
	rootEvents := protocol.NewMemorySink()
	events, err := protocol.NewScopedSink(rootEvents, "thread-1", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := NewUpdatePlan(updater, UpdatePlanOptions{Events: events})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithInvocationMetadata(context.Background(), tool.InvocationMetadata{TurnID: "turn-1"})
	result, err := executePreparedTool(t, ctx, candidate, []byte(`{"explanation":"implementation","plan":[{"step":"Inspect","status":"completed"},{"step":"Patch","status":"in_progress"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if updater.snapshot.Revision != 1 || len(updater.snapshot.Items) != 2 || updater.snapshot.Items[1].Status != plan.ItemInProgress {
		t.Fatalf("unexpected recorded plan: %#v", updater.snapshot)
	}
	if result.ToolName != "update_plan" || result.Text != "Plan updated" || result.Metadata["revision"] != int64(1) {
		t.Fatalf("unexpected Tool result: %#v", result)
	}
	published := rootEvents.Snapshot()
	if len(published) != 1 {
		t.Fatalf("unexpected events: %#v", published)
	}
}

func TestUpdatePlanRejectsMultipleInProgressItems(t *testing.T) {
	events, err := protocol.NewScopedSink(protocol.NewMemorySink(), "thread-1", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := NewUpdatePlan(&testPlanUpdater{}, UpdatePlanOptions{Events: events})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithInvocationMetadata(context.Background(), tool.InvocationMetadata{TurnID: "turn-1"})
	_, err = executePreparedTool(t, ctx, candidate, []byte(`{"plan":[{"step":"A","status":"in_progress"},{"step":"B","status":"in_progress"}]}`))
	if err == nil {
		t.Fatal("multiple in-progress items were accepted")
	}
}

func TestUpdatePlanRequiresTurnID(t *testing.T) {
	events, err := protocol.NewScopedSink(protocol.NewMemorySink(), "thread-1", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := NewUpdatePlan(&testPlanUpdater{}, UpdatePlanOptions{Events: events})
	if err != nil {
		t.Fatal(err)
	}
	_, err = executePreparedTool(t, context.Background(), candidate, []byte(`{"plan":[{"step":"A","status":"pending"}]}`))
	if err == nil {
		t.Fatal("empty turn ID was accepted")
	}
}
