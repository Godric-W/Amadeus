package plan

import (
	"testing"
	"time"
)

func TestStateAppliesValidatedSoftPlan(t *testing.T) {
	state := NewState()
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	snapshot, err := state.Apply(Update{Explanation: "implementation", Items: []Item{{Step: "Inspect", Status: ItemCompleted}, {Step: "Patch", Status: ItemInProgress}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 1 || snapshot.UpdatedAt != now || len(snapshot.Items) != 2 || state.Snapshot().Items[1].Step != "Patch" {
		t.Fatalf("unexpected plan snapshot: %#v", snapshot)
	}
}

func TestStateRejectsMultipleInProgressItems(t *testing.T) {
	_, err := NewState().Apply(Update{Items: []Item{{Step: "One", Status: ItemInProgress}, {Step: "Two", Status: ItemInProgress}}}, time.Now())
	if err == nil {
		t.Fatal("invalid plan update was accepted")
	}
}
