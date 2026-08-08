package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
)

type Store struct {
	database *Database
}

func NewStore(database *Database) (*Store, error) {
	if database == nil || database.db == nil {
		return nil, errors.New("SQLite session database is nil")
	}
	return &Store{database: database}, nil
}

func (store *Store) BeginFirstRun(ctx context.Context, input sessiondomain.BeginFirstRunInput) (sessiondomain.BeginRunResult, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	payload, err := sessiondomain.EncodePayload(sessiondomain.UserMessagePayload{Content: strings.TrimSpace(input.UserContent)})
	if err != nil {
		return sessiondomain.BeginRunResult{}, fmt.Errorf("encode first user item: %w", err)
	}
	tx, err := store.database.db.BeginTx(ctx, nil)
	if err != nil {
		return sessiondomain.BeginRunResult{}, fmt.Errorf("begin first Run SQLite transaction: %w", err)
	}
	defer tx.Rollback()

	project, err := getProjectByCanonicalPathTx(ctx, tx, input.CanonicalPath)
	if errors.Is(err, sessiondomain.ErrNotFound) {
		project, err = sessiondomain.NewProject(input.ProjectID, input.CanonicalPath, input.ProjectName, input.StartedAt)
		if err == nil {
			err = insertProject(ctx, tx, project)
		}
	} else if err == nil {
		project.UpdatedAt = input.StartedAt.UTC()
		project.LastOpenedAt = input.StartedAt.UTC()
		err = updateProjectActivity(ctx, tx, project)
	}
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}

	sessionValue, err := sessiondomain.NewSession(input.SessionID, project.ID, input.SessionTitle, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	run, err := sessiondomain.NewRun(input.RunID, sessionValue.ID, 1, input.Mode, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	applyRunMetadata(&run, input.Provider, input.Model, input.APIMode, input.Dialect)
	item, err := sessiondomain.NewRolloutItem(input.UserItemID, sessionValue.ID, run.ID, 1, sessiondomain.RolloutUserMessage, payload, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	sessionValue.NextRunSequence = 2
	sessionValue.NextItemSequence = 2
	if err := insertSession(ctx, tx, sessionValue); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	if err := insertRun(ctx, tx, run); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	if err := insertRolloutItem(ctx, tx, item); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return sessiondomain.BeginRunResult{}, fmt.Errorf("commit first Run SQLite transaction: %w", err)
	}
	return sessiondomain.BeginRunResult{Project: project, Session: sessionValue, Run: run, Item: item}, nil
}

func (store *Store) BeginRun(ctx context.Context, input sessiondomain.BeginRunInput) (sessiondomain.BeginRunResult, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	payload, err := sessiondomain.EncodePayload(sessiondomain.UserMessagePayload{Content: strings.TrimSpace(input.UserContent)})
	if err != nil {
		return sessiondomain.BeginRunResult{}, fmt.Errorf("encode user item: %w", err)
	}
	tx, err := store.database.db.BeginTx(ctx, nil)
	if err != nil {
		return sessiondomain.BeginRunResult{}, fmt.Errorf("begin Run SQLite transaction: %w", err)
	}
	defer tx.Rollback()

	sessionValue, runSequence, itemSequence, err := allocateRunAndItemSequence(ctx, tx, input.SessionID, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	project, err := getProjectByID(ctx, tx, sessionValue.ProjectID)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	run, err := sessiondomain.NewRun(input.RunID, sessionValue.ID, runSequence, input.Mode, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	applyRunMetadata(&run, input.Provider, input.Model, input.APIMode, input.Dialect)
	item, err := sessiondomain.NewRolloutItem(input.UserItemID, sessionValue.ID, run.ID, itemSequence, sessiondomain.RolloutUserMessage, payload, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	if err := insertRun(ctx, tx, run); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	if err := insertRolloutItem(ctx, tx, item); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return sessiondomain.BeginRunResult{}, fmt.Errorf("commit Run SQLite transaction: %w", err)
	}
	return sessiondomain.BeginRunResult{Project: project, Session: sessionValue, Run: run, Item: item}, nil
}

func (store *Store) AppendItems(ctx context.Context, input sessiondomain.AppendItemsInput) ([]sessiondomain.RolloutItem, error) {
	if err := store.validateContext(ctx); err != nil {
		return nil, err
	}
	if len(input.Items) == 0 {
		return []sessiondomain.RolloutItem{}, nil
	}
	tx, err := store.database.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin append rollout transaction: %w", err)
	}
	defer tx.Rollback()
	result, _, err := appendItemsTx(ctx, tx, input.SessionID, input.Items)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit append rollout transaction: %w", err)
	}
	return result, nil
}

func (store *Store) FinishRun(ctx context.Context, input sessiondomain.FinishRunInput) (sessiondomain.FinishRunResult, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.FinishRunResult{}, err
	}
	if !input.RunStatus.Terminal() {
		return sessiondomain.FinishRunResult{}, errors.New("FinishRun requires terminal status")
	}
	tx, err := store.database.db.BeginTx(ctx, nil)
	if err != nil {
		return sessiondomain.FinishRunResult{}, fmt.Errorf("begin FinishRun transaction: %w", err)
	}
	defer tx.Rollback()

	run, err := getRun(ctx, tx, input.RunID)
	if err != nil {
		return sessiondomain.FinishRunResult{}, err
	}
	if run.SessionID != input.SessionID {
		return sessiondomain.FinishRunResult{}, fmt.Errorf("%w: run %q belongs to another session", sessiondomain.ErrConflict, run.ID)
	}
	items, sessionValue, err := appendItemsTx(ctx, tx, input.SessionID, input.TerminalItems)
	if err != nil {
		return sessiondomain.FinishRunResult{}, err
	}
	if len(input.TerminalItems) == 0 {
		sessionValue, err = getSession(ctx, tx, input.SessionID)
		if err != nil {
			return sessiondomain.FinishRunResult{}, err
		}
	}
	if err := run.Finish(input.RunStatus, input.StopReason, input.UsageJSON, input.FinishedAt); err != nil {
		return sessiondomain.FinishRunResult{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE runs SET status = ?, stop_reason = ?, usage_json = ?, finished_at = ? WHERE id = ? AND status = 'running'`,
		run.Status, nullableString(run.StopReason), nullableJSON(run.UsageJSON), formatTime(*run.FinishedAt), run.ID)
	if err != nil {
		return sessiondomain.FinishRunResult{}, sqliteStoreError("finish run", err)
	}
	if err := requireOneRow(result, "finish run"); err != nil {
		return sessiondomain.FinishRunResult{}, err
	}
	if input.FinishedAt.After(sessionValue.LastActiveAt) {
		sessionValue.UpdatedAt = input.FinishedAt.UTC()
		sessionValue.LastActiveAt = input.FinishedAt.UTC()
		if err := updateSessionActivity(ctx, tx, sessionValue); err != nil {
			return sessiondomain.FinishRunResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return sessiondomain.FinishRunResult{}, fmt.Errorf("commit FinishRun transaction: %w", err)
	}
	return sessiondomain.FinishRunResult{Session: sessionValue, Run: run, Items: items}, nil
}

func (store *Store) GetProjectByCanonicalPath(ctx context.Context, canonicalPath string) (sessiondomain.Project, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.Project{}, err
	}
	return scanProject(store.database.db.QueryRowContext(ctx, `SELECT id, canonical_path, display_name, created_at, updated_at, last_opened_at FROM projects WHERE canonical_path = ?`, canonicalPath))
}

func (store *Store) GetSession(ctx context.Context, id sessiondomain.SessionID) (sessiondomain.Session, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.Session{}, err
	}
	return scanSession(store.database.db.QueryRowContext(ctx, sessionSelect+` WHERE id = ?`, id))
}

func (store *Store) ListSessions(ctx context.Context, projectID sessiondomain.ProjectID) ([]sessiondomain.Session, error) {
	if err := store.validateContext(ctx); err != nil {
		return nil, err
	}
	rows, err := store.database.db.QueryContext(ctx, sessionSelect+` WHERE project_id = ? ORDER BY last_active_at DESC, id`, projectID)
	if err != nil {
		return nil, sqliteStoreError("list sessions", err)
	}
	defer rows.Close()
	result := make([]sessiondomain.Session, 0)
	for rows.Next() {
		value, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, sqliteStoreError("iterate sessions", rows.Err())
}

func (store *Store) LatestSession(ctx context.Context, projectID sessiondomain.ProjectID) (sessiondomain.Session, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.Session{}, err
	}
	return scanSession(store.database.db.QueryRowContext(ctx, sessionSelect+` WHERE project_id = ? AND status = 'active' ORDER BY last_active_at DESC, id LIMIT 1`, projectID))
}

func (store *Store) RenameSession(ctx context.Context, input sessiondomain.RenameSessionInput) (sessiondomain.Session, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.Session{}, err
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return sessiondomain.Session{}, errors.New("session title is empty")
	}
	updatedAt := input.UpdatedAt.UTC()
	result, err := store.database.db.ExecContext(ctx, `UPDATE sessions SET title = ?, updated_at = ?, last_active_at = ? WHERE id = ?`, title, formatTime(updatedAt), formatTime(updatedAt), input.SessionID)
	if err != nil {
		return sessiondomain.Session{}, sqliteStoreError("rename session", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return sessiondomain.Session{}, sqliteStoreError("rename session rows", err)
	}
	if changed == 0 {
		return sessiondomain.Session{}, sessiondomain.ErrNotFound
	}
	return store.GetSession(ctx, input.SessionID)
}

func (store *Store) DeleteSession(ctx context.Context, id sessiondomain.SessionID) error {
	if err := store.validateContext(ctx); err != nil {
		return err
	}
	var running int
	if err := store.database.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE session_id = ? AND status = 'running'`, id).Scan(&running); err != nil {
		return sqliteStoreError("check running session", err)
	}
	if running > 0 {
		return sessiondomain.ErrConflict
	}
	result, err := store.database.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return sqliteStoreError("delete session", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return sqliteStoreError("delete session rows", err)
	}
	if changed == 0 {
		return sessiondomain.ErrNotFound
	}
	return nil
}

func (store *Store) GetRun(ctx context.Context, id sessiondomain.RunID) (sessiondomain.Run, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.Run{}, err
	}
	return scanRun(store.database.db.QueryRowContext(ctx, runSelect+` WHERE id = ?`, id))
}

func (store *Store) ListItems(ctx context.Context, sessionID sessiondomain.SessionID) ([]sessiondomain.RolloutItem, error) {
	if err := store.validateContext(ctx); err != nil {
		return nil, err
	}
	rows, err := store.database.db.QueryContext(ctx, rolloutSelect+` WHERE session_id = ? ORDER BY sequence`, sessionID)
	if err != nil {
		return nil, sqliteStoreError("list rollout items", err)
	}
	defer rows.Close()
	result := make([]sessiondomain.RolloutItem, 0)
	for rows.Next() {
		item, err := scanRolloutItem(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, sqliteStoreError("iterate rollout items", rows.Err())
}

func (store *Store) RecoverRunningRuns(ctx context.Context, sessionID sessiondomain.SessionID, at time.Time) error {
	if err := store.validateContext(ctx); err != nil {
		return err
	}
	tx, err := store.database.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin recover running Runs transaction: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, runSelect+` WHERE session_id = ? AND status = 'running' ORDER BY sequence`, sessionID)
	if err != nil {
		return sqliteStoreError("query running Runs", err)
	}
	var runs []sessiondomain.Run
	for rows.Next() {
		run, scanErr := scanRun(rows)
		if scanErr != nil {
			rows.Close()
			return scanErr
		}
		runs = append(runs, run)
	}
	if err := rows.Close(); err != nil {
		return sqliteStoreError("close running Runs", err)
	}
	itemRows, err := tx.QueryContext(ctx, rolloutSelect+` WHERE session_id = ? ORDER BY sequence`, sessionID)
	if err != nil {
		return sqliteStoreError("query rollout items for recovery", err)
	}
	allItems := make([]sessiondomain.RolloutItem, 0)
	for itemRows.Next() {
		item, scanErr := scanRolloutItem(itemRows)
		if scanErr != nil {
			itemRows.Close()
			return scanErr
		}
		allItems = append(allItems, item)
	}
	if err := itemRows.Close(); err != nil {
		return sqliteStoreError("close recovery rollout items", err)
	}
	for _, run := range runs {
		pending, pendingErr := sessiondomain.PendingToolCalls(allItems, run.ID)
		if pendingErr != nil {
			return pendingErr
		}
		drafts := make([]sessiondomain.AppendItem, 0, len(pending)+1)
		activeCalls := make([]string, 0, len(pending))
		for _, call := range pending {
			draft, draftErr := sessiondomain.InterruptedToolResultDraft(sessiondomain.RecoveryItemID(run.ID, call.ID), run.ID, call, at, "process restarted before Tool Result was recorded", "process_terminated")
			if draftErr != nil {
				return draftErr
			}
			drafts = append(drafts, draft)
			activeCalls = append(activeCalls, call.ID)
		}
		payload, encodeErr := sessiondomain.EncodePayload(sessiondomain.RunMarkerPayload{Reason: "process restarted", Guidance: "Re-plan from the current workspace state.", ActiveCalls: activeCalls})
		if encodeErr != nil {
			return encodeErr
		}
		drafts = append(drafts, sessiondomain.AppendItem{
			ID: sessiondomain.RolloutItemID("recovered-" + string(run.ID)), RunID: run.ID,
			Kind: sessiondomain.RolloutRunInterrupted, Payload: payload, CreatedAt: at,
		})
		_, _, err = appendItemsTx(ctx, tx, sessionID, drafts)
		if err != nil {
			return err
		}
		if err := run.Finish(sessiondomain.RunInterrupted, "process restarted", nil, at); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE runs SET status = ?, stop_reason = ?, finished_at = ? WHERE id = ?`, run.Status, run.StopReason, formatTime(*run.FinishedAt), run.ID); err != nil {
			return sqliteStoreError("recover running Run", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit recover running Runs transaction: %w", err)
	}
	return nil
}

func appendItemsTx(ctx context.Context, tx *sql.Tx, sessionID sessiondomain.SessionID, drafts []sessiondomain.AppendItem) ([]sessiondomain.RolloutItem, sessiondomain.Session, error) {
	sessionValue, err := getSession(ctx, tx, sessionID)
	if err != nil {
		return nil, sessiondomain.Session{}, err
	}
	if len(drafts) == 0 {
		return []sessiondomain.RolloutItem{}, sessionValue, nil
	}
	latest := sessionValue.LastActiveAt
	for _, draft := range drafts {
		if draft.CreatedAt.IsZero() {
			return nil, sessiondomain.Session{}, errors.New("rollout item created_at is zero")
		}
		if draft.CreatedAt.Before(latest) {
			return nil, sessiondomain.Session{}, errors.New("rollout item time precedes session activity")
		}
		latest = draft.CreatedAt.UTC()
		if draft.RunID != "" {
			run, err := getRun(ctx, tx, draft.RunID)
			if err != nil {
				return nil, sessiondomain.Session{}, err
			}
			if run.SessionID != sessionID {
				return nil, sessiondomain.Session{}, fmt.Errorf("%w: rollout Run belongs to another session", sessiondomain.ErrConflict)
			}
		}
	}
	start := sessionValue.NextItemSequence
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET next_item_sequence = next_item_sequence + ?, updated_at = ?, last_active_at = ? WHERE id = ? AND status = 'active'`, len(drafts), formatTime(latest), formatTime(latest), sessionID)
	if err != nil {
		return nil, sessiondomain.Session{}, sqliteStoreError("allocate rollout sequences", err)
	}
	if err := requireOneRow(result, "allocate rollout sequences"); err != nil {
		return nil, sessiondomain.Session{}, err
	}
	items := make([]sessiondomain.RolloutItem, 0, len(drafts))
	for index, draft := range drafts {
		item, err := sessiondomain.NewRolloutItem(draft.ID, sessionID, draft.RunID, start+int64(index), draft.Kind, draft.Payload, draft.CreatedAt)
		if err != nil {
			return nil, sessiondomain.Session{}, err
		}
		if err := insertRolloutItem(ctx, tx, item); err != nil {
			return nil, sessiondomain.Session{}, err
		}
		items = append(items, item)
	}
	sessionValue.NextItemSequence += int64(len(drafts))
	sessionValue.UpdatedAt = latest
	sessionValue.LastActiveAt = latest
	return items, sessionValue, nil
}

func allocateRunAndItemSequence(ctx context.Context, tx *sql.Tx, sessionID sessiondomain.SessionID, at time.Time) (sessiondomain.Session, int64, int64, error) {
	value, err := scanSession(tx.QueryRowContext(ctx, `UPDATE sessions SET next_run_sequence = next_run_sequence + 1,
        next_item_sequence = next_item_sequence + 1, updated_at = ?, last_active_at = ?
        WHERE id = ? AND status = 'active' AND updated_at <= ? AND last_active_at <= ?
        RETURNING id, project_id, title, status, next_run_sequence, next_item_sequence, created_at, updated_at, last_active_at`,
		formatTime(at), formatTime(at), sessionID, formatTime(at), formatTime(at)))
	if err != nil {
		return sessiondomain.Session{}, 0, 0, err
	}
	return value, value.NextRunSequence - 1, value.NextItemSequence - 1, nil
}

func applyRunMetadata(run *sessiondomain.Run, provider, model, apiMode, dialect string) {
	run.Provider = strings.TrimSpace(provider)
	run.Model = strings.TrimSpace(model)
	run.APIMode = strings.TrimSpace(apiMode)
	run.Dialect = strings.TrimSpace(dialect)
}

func insertProject(ctx context.Context, tx *sql.Tx, value sessiondomain.Project) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO projects(id, canonical_path, display_name, created_at, updated_at, last_opened_at) VALUES (?, ?, ?, ?, ?, ?)`,
		value.ID, value.CanonicalPath, value.DisplayName, formatTime(value.CreatedAt), formatTime(value.UpdatedAt), formatTime(value.LastOpenedAt))
	return sqliteStoreError("insert project", err)
}

func updateProjectActivity(ctx context.Context, tx *sql.Tx, value sessiondomain.Project) error {
	_, err := tx.ExecContext(ctx, `UPDATE projects SET updated_at = ?, last_opened_at = ? WHERE id = ?`, formatTime(value.UpdatedAt), formatTime(value.LastOpenedAt), value.ID)
	return sqliteStoreError("update project activity", err)
}

func insertSession(ctx context.Context, tx *sql.Tx, value sessiondomain.Session) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO sessions(id, project_id, title, status, next_run_sequence, next_item_sequence, created_at, updated_at, last_active_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.ProjectID, value.Title, value.Status, value.NextRunSequence, value.NextItemSequence,
		formatTime(value.CreatedAt), formatTime(value.UpdatedAt), formatTime(value.LastActiveAt))
	return sqliteStoreError("insert session", err)
}

func updateSessionActivity(ctx context.Context, tx *sql.Tx, value sessiondomain.Session) error {
	_, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ?, last_active_at = ? WHERE id = ?`, formatTime(value.UpdatedAt), formatTime(value.LastActiveAt), value.ID)
	return sqliteStoreError("update session activity", err)
}

func insertRun(ctx context.Context, tx *sql.Tx, value sessiondomain.Run) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO runs(id, session_id, sequence, status, stop_reason, provider, model, api_mode, dialect, run_mode, usage_json, started_at, finished_at)
        VALUES (?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, NULL, ?, NULL)`, value.ID, value.SessionID, value.Sequence, value.Status,
		nullableString(value.Provider), nullableString(value.Model), nullableString(value.APIMode), nullableString(value.Dialect), value.Mode, formatTime(value.StartedAt))
	return sqliteStoreError("insert run", err)
}

func insertRolloutItem(ctx context.Context, tx *sql.Tx, value sessiondomain.RolloutItem) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO rollout_items(id, session_id, run_id, sequence, kind, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.SessionID, nullableRunID(value.RunID), value.Sequence, value.Kind, string(value.PayloadJSON), formatTime(value.CreatedAt))
	return sqliteStoreError("insert rollout item", err)
}

func getProjectByCanonicalPathTx(ctx context.Context, tx *sql.Tx, path string) (sessiondomain.Project, error) {
	return scanProject(tx.QueryRowContext(ctx, `SELECT id, canonical_path, display_name, created_at, updated_at, last_opened_at FROM projects WHERE canonical_path = ?`, path))
}

func getProjectByID(ctx context.Context, tx *sql.Tx, id sessiondomain.ProjectID) (sessiondomain.Project, error) {
	return scanProject(tx.QueryRowContext(ctx, `SELECT id, canonical_path, display_name, created_at, updated_at, last_opened_at FROM projects WHERE id = ?`, id))
}

func getSession(ctx context.Context, tx *sql.Tx, id sessiondomain.SessionID) (sessiondomain.Session, error) {
	return scanSession(tx.QueryRowContext(ctx, sessionSelect+` WHERE id = ?`, id))
}

func getRun(ctx context.Context, tx *sql.Tx, id sessiondomain.RunID) (sessiondomain.Run, error) {
	return scanRun(tx.QueryRowContext(ctx, runSelect+` WHERE id = ?`, id))
}

func (store *Store) validateContext(ctx context.Context) error {
	if store == nil || store.database == nil || store.database.db == nil {
		return errors.New("SQLite session store is nil")
	}
	if ctx == nil {
		return errors.New("SQLite session store context is nil")
	}
	return ctx.Err()
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func nullableRunID(value sessiondomain.RunID) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func sqliteStoreError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", sessiondomain.ErrNotFound, operation)
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "constraint failed") || strings.Contains(lower, "unique constraint") || strings.Contains(lower, "foreign key constraint") {
		return fmt.Errorf("%w: %s: %v", sessiondomain.ErrConflict, operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func requireOneRow(result sql.Result, operation string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect %s rows affected: %w", operation, err)
	}
	if rows != 1 {
		return fmt.Errorf("%w: %s affected %d rows", sessiondomain.ErrConflict, operation, rows)
	}
	return nil
}
