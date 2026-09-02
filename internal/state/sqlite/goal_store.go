package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/google/uuid"
)

type goalStore struct {
	db    *sql.DB
	clock func() time.Time
}

func (store *goalStore) Get(ctx context.Context, threadID protocol.ThreadID) (*state.ThreadGoal, error) {
	if threadID.IsZero() {
		return nil, errors.New("goal thread ID is empty")
	}
	goal, err := scanGoal(store.db.QueryRowContext(ctx, goalSelect+` WHERE thread_id = ?`, threadID.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read thread goal: %w", err)
	}
	return &goal, nil
}

func (store *goalStore) Replace(ctx context.Context, threadID protocol.ThreadID, objective string, status protocol.ThreadGoalStatus, budget *int64) (state.ThreadGoal, error) {
	if err := validateGoalInput(threadID, objective, status, budget); err != nil {
		return state.ThreadGoal{}, err
	}
	now := store.clock().UTC()
	status = statusAfterBudget(status, 0, budget)
	goal, err := scanGoal(store.db.QueryRowContext(ctx, `INSERT INTO thread_goals (
		thread_id, goal_id, objective, status, token_budget, tokens_used, time_used_seconds, created_at_ms, updated_at_ms
	) VALUES (?, ?, ?, ?, ?, 0, 0, ?, ?)
	ON CONFLICT(thread_id) DO UPDATE SET
		goal_id = excluded.goal_id, objective = excluded.objective, status = excluded.status,
		token_budget = excluded.token_budget, tokens_used = 0, time_used_seconds = 0,
		created_at_ms = excluded.created_at_ms, updated_at_ms = excluded.updated_at_ms
	RETURNING `+goalColumns, threadID.String(), uuid.New().String(), objective, string(status), budgetValue(budget), epochMillis(now), epochMillis(now)))
	if err != nil {
		return state.ThreadGoal{}, fmt.Errorf("replace thread goal: %w", err)
	}
	return goal, nil
}

func (store *goalStore) InsertIfAbsentOrComplete(ctx context.Context, threadID protocol.ThreadID, objective string, status protocol.ThreadGoalStatus, budget *int64) (*state.ThreadGoal, error) {
	if err := validateGoalInput(threadID, objective, status, budget); err != nil {
		return nil, err
	}
	now := store.clock().UTC()
	status = statusAfterBudget(status, 0, budget)
	goal, err := scanGoal(store.db.QueryRowContext(ctx, `INSERT INTO thread_goals (
		thread_id, goal_id, objective, status, token_budget, tokens_used, time_used_seconds, created_at_ms, updated_at_ms
	) VALUES (?, ?, ?, ?, ?, 0, 0, ?, ?)
	ON CONFLICT(thread_id) DO UPDATE SET
		goal_id = excluded.goal_id, objective = excluded.objective, status = excluded.status,
		token_budget = excluded.token_budget, tokens_used = 0, time_used_seconds = 0,
		created_at_ms = excluded.created_at_ms, updated_at_ms = excluded.updated_at_ms
	WHERE thread_goals.status = 'complete'
	RETURNING `+goalColumns, threadID.String(), uuid.New().String(), objective, string(status), budgetValue(budget), epochMillis(now), epochMillis(now)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("insert thread goal: %w", err)
	}
	return &goal, nil
}

func (store *goalStore) Update(ctx context.Context, threadID protocol.ThreadID, update state.GoalUpdate) (*state.ThreadGoal, error) {
	if threadID.IsZero() {
		return nil, errors.New("goal thread ID is empty")
	}
	if update.Objective != nil {
		value := strings.TrimSpace(*update.Objective)
		if err := protocol.ValidateThreadGoalObjective(value); err != nil {
			return nil, err
		}
		update.Objective = &value
	}
	if update.Status != nil && !update.Status.Valid() {
		return nil, errors.New("goal update status is invalid")
	}
	if update.TokenBudget.Set && update.TokenBudget.Value != nil && *update.TokenBudget.Value <= 0 {
		return nil, errors.New("goal token budget must be positive")
	}
	if update.Objective == nil && update.Status == nil && !update.TokenBudget.Set {
		goal, err := store.Get(ctx, threadID)
		if err != nil || goal == nil || update.ExpectedGoalID == "" || goal.GoalID == update.ExpectedGoalID {
			return goal, err
		}
		return nil, nil
	}
	nowMS := epochMillis(store.clock().UTC())
	var result sql.Result
	var err error
	switch {
	case update.Status != nil && update.TokenBudget.Set:
		result, err = store.db.ExecContext(ctx, `UPDATE thread_goals SET
			objective = COALESCE(?, objective),
			status = CASE
				WHEN status = 'budget_limited' AND ? IN ('paused', 'blocked') THEN status
				WHEN ? = 'active' AND ? IS NOT NULL AND tokens_used >= ? THEN 'budget_limited'
				ELSE ?
			END,
			token_budget = ?, updated_at_ms = ?
		WHERE thread_id = ? AND (? = '' OR goal_id = ?)`, nullableString(update.Objective), string(*update.Status), string(*update.Status), budgetValue(update.TokenBudget.Value), budgetValue(update.TokenBudget.Value), string(*update.Status), budgetValue(update.TokenBudget.Value), nowMS, threadID.String(), update.ExpectedGoalID, update.ExpectedGoalID)
	case update.Status != nil:
		result, err = store.db.ExecContext(ctx, `UPDATE thread_goals SET
			objective = COALESCE(?, objective),
			status = CASE
				WHEN status = 'budget_limited' AND ? IN ('paused', 'blocked') THEN status
				WHEN ? = 'active' AND token_budget IS NOT NULL AND tokens_used >= token_budget THEN 'budget_limited'
				ELSE ?
			END,
			updated_at_ms = ?
		WHERE thread_id = ? AND (? = '' OR goal_id = ?)`, nullableString(update.Objective), string(*update.Status), string(*update.Status), string(*update.Status), nowMS, threadID.String(), update.ExpectedGoalID, update.ExpectedGoalID)
	case update.TokenBudget.Set:
		result, err = store.db.ExecContext(ctx, `UPDATE thread_goals SET
			objective = COALESCE(?, objective), token_budget = ?,
			status = CASE WHEN status = 'active' AND ? IS NOT NULL AND tokens_used >= ? THEN 'budget_limited' ELSE status END,
			updated_at_ms = ?
		WHERE thread_id = ? AND (? = '' OR goal_id = ?)`, nullableString(update.Objective), budgetValue(update.TokenBudget.Value), budgetValue(update.TokenBudget.Value), budgetValue(update.TokenBudget.Value), nowMS, threadID.String(), update.ExpectedGoalID, update.ExpectedGoalID)
	default:
		result, err = store.db.ExecContext(ctx, `UPDATE thread_goals SET objective = ?, updated_at_ms = ? WHERE thread_id = ? AND (? = '' OR goal_id = ?)`, *update.Objective, nowMS, threadID.String(), update.ExpectedGoalID, update.ExpectedGoalID)
	}
	if err != nil {
		return nil, fmt.Errorf("update thread goal: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, nil
	}
	return store.Get(ctx, threadID)
}

func (store *goalStore) PauseActive(ctx context.Context, threadID protocol.ThreadID) (*state.ThreadGoal, error) {
	return store.updateActiveStatus(ctx, threadID, protocol.ThreadGoalPaused)
}

func (store *goalStore) UsageLimitActive(ctx context.Context, threadID protocol.ThreadID) (*state.ThreadGoal, error) {
	return store.updateActiveStatus(ctx, threadID, protocol.ThreadGoalUsageLimited)
}

func (store *goalStore) updateActiveStatus(ctx context.Context, threadID protocol.ThreadID, status protocol.ThreadGoalStatus) (*state.ThreadGoal, error) {
	result, err := store.db.ExecContext(ctx, `UPDATE thread_goals SET status = ?, updated_at_ms = ?
		WHERE thread_id = ? AND (status = 'active' OR (? = 'usage_limited' AND status = 'budget_limited'))`, string(status), epochMillis(store.clock().UTC()), threadID.String(), string(status))
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return nil, err
	}
	return store.Get(ctx, threadID)
}

func (store *goalStore) Delete(ctx context.Context, threadID protocol.ThreadID) (*state.ThreadGoal, error) {
	goal, err := scanGoal(store.db.QueryRowContext(ctx, `DELETE FROM thread_goals WHERE thread_id = ? RETURNING `+goalColumns, threadID.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("delete thread goal: %w", err)
	}
	return &goal, nil
}

func (store *goalStore) AccountUsage(ctx context.Context, threadID protocol.ThreadID, timeDelta, tokenDelta int64, mode state.GoalAccountingMode, expectedGoalID string) (state.GoalAccountingOutcome, error) {
	timeDelta = max(timeDelta, 0)
	tokenDelta = max(tokenDelta, 0)
	if timeDelta == 0 && tokenDelta == 0 {
		goal, err := store.Get(ctx, threadID)
		return state.GoalAccountingOutcome{Goal: goal}, err
	}
	statusFilter, budgetFilter, err := accountingFilters(mode)
	if err != nil {
		return state.GoalAccountingOutcome{}, err
	}
	query := `UPDATE thread_goals SET
		time_used_seconds = time_used_seconds + ?, tokens_used = tokens_used + ?,
		status = CASE WHEN ` + budgetFilter + ` AND token_budget IS NOT NULL AND tokens_used + ? >= token_budget THEN 'budget_limited' ELSE status END,
		updated_at_ms = ?
		WHERE thread_id = ? AND ` + statusFilter
	args := []any{timeDelta, tokenDelta, tokenDelta, epochMillis(store.clock().UTC()), threadID.String()}
	if expectedGoalID != "" {
		query += ` AND goal_id = ?`
		args = append(args, expectedGoalID)
	}
	query += ` RETURNING ` + goalColumns
	goal, scanErr := scanGoal(store.db.QueryRowContext(ctx, query, args...))
	if errors.Is(scanErr, sql.ErrNoRows) {
		current, getErr := store.Get(ctx, threadID)
		return state.GoalAccountingOutcome{Goal: current}, getErr
	}
	if scanErr != nil {
		return state.GoalAccountingOutcome{}, fmt.Errorf("account thread goal usage: %w", scanErr)
	}
	return state.GoalAccountingOutcome{Goal: &goal, Updated: true}, nil
}

func (store *goalStore) ReplaceSnapshot(ctx context.Context, goal state.ThreadGoal) error {
	if err := goal.Validate(); err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO thread_goals (`+goalColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(thread_id) DO UPDATE SET goal_id=excluded.goal_id, objective=excluded.objective, status=excluded.status,
		token_budget=excluded.token_budget, tokens_used=excluded.tokens_used, time_used_seconds=excluded.time_used_seconds,
		created_at_ms=excluded.created_at_ms, updated_at_ms=excluded.updated_at_ms`, goal.ThreadID.String(), goal.GoalID, goal.Objective, string(goal.Status), budgetValue(goal.TokenBudget), goal.TokensUsed, goal.TimeUsedSeconds, epochMillis(goal.CreatedAt), epochMillis(goal.UpdatedAt))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO thread_goal_continuation_deferrals(thread_id) VALUES (?) ON CONFLICT(thread_id) DO NOTHING`, goal.ThreadID.String()); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *goalStore) HasContinuationDeferral(ctx context.Context, threadID protocol.ThreadID) (bool, error) {
	var exists bool
	err := store.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM thread_goal_continuation_deferrals WHERE thread_id = ?)`, threadID.String()).Scan(&exists)
	return exists, err
}

func (store *goalStore) ClearContinuationDeferral(ctx context.Context, threadID protocol.ThreadID) error {
	_, err := store.db.ExecContext(ctx, `DELETE FROM thread_goal_continuation_deferrals WHERE thread_id = ?`, threadID.String())
	return err
}

func accountingFilters(mode state.GoalAccountingMode) (string, string, error) {
	stopped := `status IN ('active', 'paused', 'blocked', 'usage_limited', 'budget_limited')`
	switch mode {
	case state.GoalAccountingActiveStatusOnly:
		return `status = 'active'`, `status = 'active'`, nil
	case state.GoalAccountingActiveOnly:
		return `status IN ('active', 'budget_limited')`, `status = 'active'`, nil
	case state.GoalAccountingActiveOrComplete:
		return `status IN ('active', 'budget_limited', 'complete')`, `status = 'active'`, nil
	case state.GoalAccountingActiveOrStopped:
		return stopped, stopped, nil
	default:
		return "", "", errors.New("goal accounting mode is invalid")
	}
}

const goalColumns = `thread_id, goal_id, objective, status, token_budget, tokens_used, time_used_seconds, created_at_ms, updated_at_ms`
const goalSelect = `SELECT ` + goalColumns + ` FROM thread_goals`

func scanGoal(scanner rowScanner) (state.ThreadGoal, error) {
	var goal state.ThreadGoal
	var threadID, status string
	var budget sql.NullInt64
	var createdMS, updatedMS int64
	if err := scanner.Scan(&threadID, &goal.GoalID, &goal.Objective, &status, &budget, &goal.TokensUsed, &goal.TimeUsedSeconds, &createdMS, &updatedMS); err != nil {
		return state.ThreadGoal{}, err
	}
	parsed, err := protocol.ParseThreadID(threadID)
	if err != nil {
		return state.ThreadGoal{}, err
	}
	goal.ThreadID = parsed
	goal.Status = protocol.ThreadGoalStatus(status)
	if budget.Valid {
		goal.TokenBudget = &budget.Int64
	}
	goal.CreatedAt = time.UnixMilli(createdMS).UTC()
	goal.UpdatedAt = time.UnixMilli(updatedMS).UTC()
	return goal, goal.Validate()
}

func validateGoalInput(threadID protocol.ThreadID, objective string, status protocol.ThreadGoalStatus, budget *int64) error {
	if threadID.IsZero() {
		return errors.New("goal thread ID is empty")
	}
	if err := protocol.ValidateThreadGoalObjective(objective); err != nil {
		return err
	}
	if !status.Valid() {
		return errors.New("goal status is invalid")
	}
	if budget != nil && *budget <= 0 {
		return errors.New("goal token budget must be positive")
	}
	return nil
}

func statusAfterBudget(status protocol.ThreadGoalStatus, used int64, budget *int64) protocol.ThreadGoalStatus {
	if status == protocol.ThreadGoalActive && budget != nil && used >= *budget {
		return protocol.ThreadGoalBudgetLimited
	}
	return status
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func budgetValue(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func epochMillis(value time.Time) int64 { return value.UTC().UnixMilli() }

var _ state.GoalStore = (*goalStore)(nil)
