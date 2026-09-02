package state

import (
	"context"
	"errors"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

type Runtime interface {
	Threads() threadstore.MetadataDB
	Goals() GoalStore
	Close() error
}

type ThreadGoal struct {
	ThreadID        protocol.ThreadID
	GoalID          string
	Objective       string
	Status          protocol.ThreadGoalStatus
	TokenBudget     *int64
	TokensUsed      int64
	TimeUsedSeconds int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (goal ThreadGoal) Validate() error {
	if goal.ThreadID.IsZero() || goal.GoalID == "" {
		return errors.New("stored thread goal identity is incomplete")
	}
	public := goal.Protocol()
	return public.Validate()
}

func (goal ThreadGoal) Protocol() protocol.ThreadGoal {
	return protocol.ThreadGoal{
		ThreadID: goal.ThreadID, Objective: goal.Objective, Status: goal.Status,
		TokenBudget: cloneInt64(goal.TokenBudget), TokensUsed: goal.TokensUsed,
		TimeUsedSeconds: goal.TimeUsedSeconds,
		CreatedAt:       goal.CreatedAt.UTC().Unix(), UpdatedAt: goal.UpdatedAt.UTC().Unix(),
	}
}

type GoalUpdate struct {
	Objective      *string
	Status         *protocol.ThreadGoalStatus
	TokenBudget    TokenBudgetUpdate
	ExpectedGoalID string
}

type TokenBudgetUpdate struct {
	Set   bool
	Value *int64
}

type GoalAccountingMode string

const (
	GoalAccountingActiveStatusOnly GoalAccountingMode = "active_status_only"
	GoalAccountingActiveOnly       GoalAccountingMode = "active_only"
	GoalAccountingActiveOrComplete GoalAccountingMode = "active_or_complete"
	GoalAccountingActiveOrStopped  GoalAccountingMode = "active_or_stopped"
)

type GoalAccountingOutcome struct {
	Goal    *ThreadGoal
	Updated bool
}

type GoalStore interface {
	Get(context.Context, protocol.ThreadID) (*ThreadGoal, error)
	Replace(context.Context, protocol.ThreadID, string, protocol.ThreadGoalStatus, *int64) (ThreadGoal, error)
	InsertIfAbsentOrComplete(context.Context, protocol.ThreadID, string, protocol.ThreadGoalStatus, *int64) (*ThreadGoal, error)
	Update(context.Context, protocol.ThreadID, GoalUpdate) (*ThreadGoal, error)
	PauseActive(context.Context, protocol.ThreadID) (*ThreadGoal, error)
	UsageLimitActive(context.Context, protocol.ThreadID) (*ThreadGoal, error)
	Delete(context.Context, protocol.ThreadID) (*ThreadGoal, error)
	AccountUsage(context.Context, protocol.ThreadID, int64, int64, GoalAccountingMode, string) (GoalAccountingOutcome, error)
	ReplaceSnapshot(context.Context, ThreadGoal) error
	HasContinuationDeferral(context.Context, protocol.ThreadID) (bool, error)
	ClearContinuationDeferral(context.Context, protocol.ThreadID) error
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
