package protocol

import (
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/testutil"
)

func TestThreadGoalEventRoundTrip(t *testing.T) {
	budget := int64(100)
	goal := ThreadGoal{
		ThreadID: testutil.ThreadID(1), Objective: "ship Goal mode", Status: ThreadGoalActive,
		TokenBudget: &budget, TokensUsed: 25, TimeUsedSeconds: 3, CreatedAt: 1, UpdatedAt: 2,
	}
	encoded, err := EncodeEventMsg(ThreadGoalUpdatedEvent{ThreadID: goal.ThreadID, TurnID: "turn-1", Goal: goal})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeEventMsg(encoded)
	if err != nil {
		t.Fatal(err)
	}
	update, ok := decoded.(ThreadGoalUpdatedEvent)
	if !ok || update.Goal.Objective != goal.Objective || update.Goal.TokenBudget == nil || *update.Goal.TokenBudget != budget {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestThreadGoalObjectiveValidation(t *testing.T) {
	if err := ValidateThreadGoalObjective(""); err == nil {
		t.Fatal("empty objective accepted")
	}
	if err := ValidateThreadGoalObjective(strings.Repeat("界", MaxThreadGoalObjectiveChars)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateThreadGoalObjective(strings.Repeat("界", MaxThreadGoalObjectiveChars+1)); err == nil {
		t.Fatal("oversized objective accepted")
	}
}
