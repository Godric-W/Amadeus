package builtin

import (
	"context"
	"errors"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestUpdatePlanPublishesTransientUpdate(t *testing.T) {
	rootEvents := protocol.NewMemorySink()
	events, err := protocol.NewScopedSink(rootEvents, "submission-1", testutil.ThreadID(1), "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := NewUpdatePlan(UpdatePlanOptions{Events: events})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithInvocationMetadata(context.Background(), tool.InvocationMetadata{SessionID: testutil.SessionID(1), ThreadID: testutil.ThreadID(1), TurnID: "turn-1"})
	result, err := executePreparedTool(t, ctx, candidate, []byte(`{"explanation":"implementation","plan":[{"step":"Inspect","status":"completed"},{"step":"Patch","status":"in_progress"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.ToolName != "update_plan" || result.Text != "Plan updated" {
		t.Fatalf("unexpected Tool result: %#v", result)
	}
	published := rootEvents.Snapshot()
	if len(published) != 1 {
		t.Fatalf("unexpected events: %#v", published)
	}
	update, ok := published[0].Msg.(protocol.PlanUpdateEvent)
	if !ok || update.ThreadID != testutil.ThreadID(1) || update.TurnID != "turn-1" || len(update.Plan) != 2 || update.Plan[1].Status != protocol.StepInProgress {
		t.Fatalf("unexpected plan update: %#v", published[0].Msg)
	}
}

func TestUpdatePlanAllowsEmptyPlan(t *testing.T) {
	events, err := protocol.NewScopedSink(protocol.NewMemorySink(), "submission-1", testutil.ThreadID(1), "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := NewUpdatePlan(UpdatePlanOptions{Events: events})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithInvocationMetadata(context.Background(), tool.InvocationMetadata{SessionID: testutil.SessionID(1), ThreadID: testutil.ThreadID(1), TurnID: "turn-1"})
	if _, err := executePreparedTool(t, ctx, candidate, []byte(`{"plan":[]}`)); err != nil {
		t.Fatalf("empty plan was rejected: %v", err)
	}
}

func TestUpdatePlanRejectsMultipleInProgressItems(t *testing.T) {
	events, err := protocol.NewScopedSink(protocol.NewMemorySink(), "submission-1", testutil.ThreadID(1), "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := NewUpdatePlan(UpdatePlanOptions{Events: events})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithInvocationMetadata(context.Background(), tool.InvocationMetadata{SessionID: testutil.SessionID(1), ThreadID: testutil.ThreadID(1), TurnID: "turn-1"})
	_, err = executePreparedTool(t, ctx, candidate, []byte(`{"plan":[{"step":"A","status":"in_progress"},{"step":"B","status":"in_progress"}]}`))
	if err == nil {
		t.Fatal("multiple in-progress items were accepted")
	}
}

func TestUpdatePlanRequiresTurnID(t *testing.T) {
	candidate, err := NewUpdatePlan(UpdatePlanOptions{Events: protocol.NewMemorySink()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithInvocationMetadata(context.Background(), tool.InvocationMetadata{SessionID: testutil.SessionID(1), ThreadID: testutil.ThreadID(1)})
	_, err = executePreparedTool(t, ctx, candidate, []byte(`{"plan":[{"step":"A","status":"pending"}]}`))
	if err == nil {
		t.Fatal("empty turn ID was accepted")
	}
}

func TestUpdatePlanReturnsPublishFailure(t *testing.T) {
	candidate, err := NewUpdatePlan(UpdatePlanOptions{Events: failingPlanEventSink{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithInvocationMetadata(context.Background(), tool.InvocationMetadata{SessionID: testutil.SessionID(1), ThreadID: testutil.ThreadID(1), TurnID: "turn-1"})
	if _, err := executePreparedTool(t, ctx, candidate, []byte(`{"plan":[]}`)); err == nil {
		t.Fatal("publish failure was ignored")
	}
}

type failingPlanEventSink struct{}

func (failingPlanEventSink) Publish(context.Context, protocol.Event) error {
	return errors.New("publish failed")
}
