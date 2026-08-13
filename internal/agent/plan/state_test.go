package plan

import (
	"errors"
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

func TestStateDoesNotCommitWhenPersistenceFails(t *testing.T) {
	state := NewState()
	want := errors.New("persist failed")
	_, err := state.ApplyPersistent(Update{Items: []Item{{Step: "Inspect", Status: ItemInProgress}}}, time.Now(), func(Snapshot) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("unexpected persistence error: %v", err)
	}
	if snapshot := state.Snapshot(); snapshot.Revision != 0 || len(snapshot.Items) != 0 {
		t.Fatalf("failed persistence committed state: %#v", snapshot)
	}
}

func TestStateRestoreContinuesRevision(t *testing.T) {
	state := NewState()
	restored := Snapshot{
		Explanation: "resume", Items: []Item{{Step: "Patch", Status: ItemInProgress}},
		UpdatedAt: time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC), Revision: 4,
	}
	if err := state.Restore(restored); err != nil {
		t.Fatal(err)
	}
	next, err := state.Apply(Update{Items: []Item{{Step: "Patch", Status: ItemCompleted}}}, time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if next.Revision != 5 || next.Items[0].Status != ItemCompleted {
		t.Fatalf("unexpected restored revision: %#v", next)
	}
}

func TestStateRejectsMultipleInProgressItems(t *testing.T) {
	_, err := NewState().Apply(Update{Items: []Item{{Step: "One", Status: ItemInProgress}, {Step: "Two", Status: ItemInProgress}}}, time.Now())
	if err == nil {
		t.Fatal("invalid plan update was accepted")
	}
}
