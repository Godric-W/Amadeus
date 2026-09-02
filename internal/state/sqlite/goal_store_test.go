package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

func TestGoalStoreCreateAccountCompleteAndReplace(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	runtime, err := Open(ctx, t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	goals := runtime.Goals()
	threadID := testutil.ThreadID(1)
	budget := int64(25)
	created, err := goals.InsertIfAbsentOrComplete(ctx, threadID, "ship goal store", protocol.ThreadGoalActive, &budget)
	if err != nil {
		t.Fatal(err)
	}
	if created == nil || created.Status != protocol.ThreadGoalActive || created.GoalID == "" {
		t.Fatalf("created = %#v", created)
	}
	if duplicate, err := goals.InsertIfAbsentOrComplete(ctx, threadID, "replace unfinished", protocol.ThreadGoalActive, nil); err != nil || duplicate != nil {
		t.Fatalf("duplicate=%#v err=%v", duplicate, err)
	}
	now = now.Add(2 * time.Second)
	accounted, err := goals.AccountUsage(ctx, threadID, 2, 30, state.GoalAccountingActiveOnly, created.GoalID)
	if err != nil {
		t.Fatal(err)
	}
	if !accounted.Updated || accounted.Goal.Status != protocol.ThreadGoalBudgetLimited || accounted.Goal.TokensUsed != 30 || accounted.Goal.TimeUsedSeconds != 2 {
		t.Fatalf("accounted = %#v", accounted)
	}
	complete := protocol.ThreadGoalComplete
	completed, err := goals.Update(ctx, threadID, state.GoalUpdate{Status: &complete, ExpectedGoalID: created.GoalID})
	if err != nil {
		t.Fatal(err)
	}
	if completed == nil || completed.Status != protocol.ThreadGoalComplete {
		t.Fatalf("completed = %#v", completed)
	}
	replacement, err := goals.InsertIfAbsentOrComplete(ctx, threadID, "new goal", protocol.ThreadGoalActive, nil)
	if err != nil {
		t.Fatal(err)
	}
	if replacement == nil || replacement.GoalID == created.GoalID || replacement.TokensUsed != 0 || replacement.TimeUsedSeconds != 0 {
		t.Fatalf("replacement = %#v", replacement)
	}
}

func TestGoalStoreCASBudgetAndDeferralContracts(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)
	runtime, err := Open(ctx, t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	goals := runtime.Goals()
	threadID := testutil.ThreadID(2)
	goal, err := goals.Replace(ctx, threadID, "original", protocol.ThreadGoalActive, nil)
	if err != nil {
		t.Fatal(err)
	}
	wrong := state.GoalUpdate{Objective: pointer("stale"), ExpectedGoalID: "wrong-goal"}
	if updated, err := goals.Update(ctx, threadID, wrong); err != nil || updated != nil {
		t.Fatalf("stale update=%#v err=%v", updated, err)
	}
	budget := int64(10)
	updated, err := goals.Update(ctx, threadID, state.GoalUpdate{TokenBudget: state.TokenBudgetUpdate{Set: true, Value: &budget}, ExpectedGoalID: goal.GoalID})
	if err != nil {
		t.Fatal(err)
	}
	if updated == nil || updated.TokenBudget == nil || *updated.TokenBudget != 10 {
		t.Fatalf("budget update = %#v", updated)
	}
	if _, err := goals.AccountUsage(ctx, threadID, 0, 10, state.GoalAccountingActiveOnly, goal.GoalID); err != nil {
		t.Fatal(err)
	}
	active := protocol.ThreadGoalActive
	stillLimited, err := goals.Update(ctx, threadID, state.GoalUpdate{Status: &active, ExpectedGoalID: goal.GoalID})
	if err != nil {
		t.Fatal(err)
	}
	if stillLimited.Status != protocol.ThreadGoalBudgetLimited {
		t.Fatalf("status = %s", stillLimited.Status)
	}
	resumed, err := goals.Update(ctx, threadID, state.GoalUpdate{Status: &active, TokenBudget: state.TokenBudgetUpdate{Set: true}, ExpectedGoalID: goal.GoalID})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != protocol.ThreadGoalActive || resumed.TokenBudget != nil {
		t.Fatalf("resumed = %#v", resumed)
	}
	if err := goals.ReplaceSnapshot(ctx, *resumed); err != nil {
		t.Fatal(err)
	}
	if deferred, err := goals.HasContinuationDeferral(ctx, threadID); err != nil || !deferred {
		t.Fatalf("deferred=%v err=%v", deferred, err)
	}
	if _, err := goals.Delete(ctx, threadID); err != nil {
		t.Fatal(err)
	}
	if deferred, err := goals.HasContinuationDeferral(ctx, threadID); err != nil || deferred {
		t.Fatalf("deferred after delete=%v err=%v", deferred, err)
	}
}

func TestGoalStoreStoppedAccountingModes(t *testing.T) {
	ctx := context.Background()
	runtime, err := Open(ctx, t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	goals := runtime.Goals()
	threadID := testutil.ThreadID(3)
	goal, err := goals.Replace(ctx, threadID, "blocked work", protocol.ThreadGoalActive, nil)
	if err != nil {
		t.Fatal(err)
	}
	blocked := protocol.ThreadGoalBlocked
	if _, err := goals.Update(ctx, threadID, state.GoalUpdate{Status: &blocked, ExpectedGoalID: goal.GoalID}); err != nil {
		t.Fatal(err)
	}
	unchanged, err := goals.AccountUsage(ctx, threadID, 1, 5, state.GoalAccountingActiveOnly, goal.GoalID)
	if err != nil || unchanged.Updated {
		t.Fatalf("active-only = %#v err=%v", unchanged, err)
	}
	accounted, err := goals.AccountUsage(ctx, threadID, 1, 5, state.GoalAccountingActiveOrStopped, goal.GoalID)
	if err != nil || !accounted.Updated || accounted.Goal.Status != protocol.ThreadGoalBlocked || accounted.Goal.TokensUsed != 5 {
		t.Fatalf("stopped accounting = %#v err=%v", accounted, err)
	}
}

func pointer(value string) *string { return &value }
