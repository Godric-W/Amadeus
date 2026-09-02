package protocol

import (
	"errors"
	"strings"
)

const MaxThreadGoalObjectiveChars = 4_000

type ThreadGoalStatus string

const (
	ThreadGoalActive        ThreadGoalStatus = "active"
	ThreadGoalPaused        ThreadGoalStatus = "paused"
	ThreadGoalBlocked       ThreadGoalStatus = "blocked"
	ThreadGoalUsageLimited  ThreadGoalStatus = "usage_limited"
	ThreadGoalBudgetLimited ThreadGoalStatus = "budget_limited"
	ThreadGoalComplete      ThreadGoalStatus = "complete"
)

func (status ThreadGoalStatus) Valid() bool {
	switch status {
	case ThreadGoalActive, ThreadGoalPaused, ThreadGoalBlocked, ThreadGoalUsageLimited, ThreadGoalBudgetLimited, ThreadGoalComplete:
		return true
	default:
		return false
	}
}

func (status ThreadGoalStatus) IsActive() bool { return status == ThreadGoalActive }

func (status ThreadGoalStatus) IsStopped() bool { return status.Valid() && status != ThreadGoalActive }

func ValidateThreadGoalObjective(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("goal objective must not be empty")
	}
	if len([]rune(value)) > MaxThreadGoalObjectiveChars {
		return errors.New("goal objective must be at most 4000 characters")
	}
	return nil
}

type ThreadGoal struct {
	ThreadID        ThreadID         `json:"thread_id"`
	Objective       string           `json:"objective"`
	Status          ThreadGoalStatus `json:"status"`
	TokenBudget     *int64           `json:"token_budget,omitempty"`
	TokensUsed      int64            `json:"tokens_used"`
	TimeUsedSeconds int64            `json:"time_used_seconds"`
	CreatedAt       int64            `json:"created_at"`
	UpdatedAt       int64            `json:"updated_at"`
}

type ThreadGoalUpdatedEvent struct {
	ThreadID ThreadID   `json:"thread_id"`
	TurnID   TurnID     `json:"turn_id,omitempty"`
	Goal     ThreadGoal `json:"goal"`
}

func (ThreadGoalUpdatedEvent) isEventMsg() {}

func (event ThreadGoalUpdatedEvent) Validate() error {
	if event.ThreadID.IsZero() || event.Goal.ThreadID != event.ThreadID {
		return errors.New("thread goal update identity is inconsistent")
	}
	return event.Goal.Validate()
}

type ThreadGoalClearedEvent struct {
	ThreadID ThreadID `json:"thread_id"`
}

func (ThreadGoalClearedEvent) isEventMsg() {}

func (event ThreadGoalClearedEvent) Validate() error {
	if event.ThreadID.IsZero() {
		return errors.New("thread goal clear thread ID is empty")
	}
	return nil
}

func (goal ThreadGoal) Validate() error {
	if goal.ThreadID.IsZero() {
		return errors.New("thread goal thread ID is empty")
	}
	if err := ValidateThreadGoalObjective(goal.Objective); err != nil {
		return err
	}
	if !goal.Status.Valid() {
		return errors.New("thread goal status is invalid")
	}
	if goal.TokenBudget != nil && *goal.TokenBudget <= 0 {
		return errors.New("thread goal token budget must be positive")
	}
	if goal.TokensUsed < 0 || goal.TimeUsedSeconds < 0 {
		return errors.New("thread goal usage is negative")
	}
	if goal.CreatedAt <= 0 || goal.UpdatedAt < goal.CreatedAt {
		return errors.New("thread goal timestamps are invalid")
	}
	return nil
}
