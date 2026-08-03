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
		return nil, errors.New("SQLite session store database is nil")
	}
	return &Store{database: database}, nil
}

func (store *Store) BeginFirstRun(ctx context.Context, input sessiondomain.BeginFirstRunInput) (sessiondomain.BeginRunResult, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	if input.ContextFromRunID != "" {
		return sessiondomain.BeginRunResult{}, fmt.Errorf("%w: a new conversation session cannot reference an interrupted run", sessiondomain.ErrConflict)
	}
	candidate, err := sessiondomain.NewProject(input.ProjectID, input.CanonicalPath, input.ProjectName, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	tx, err := store.database.db.BeginTx(ctx, nil)
	if err != nil {
		return sessiondomain.BeginRunResult{}, fmt.Errorf("begin first Run SQLite transaction: %w", err)
	}
	defer tx.Rollback()
	project, err := upsertProject(ctx, tx, candidate, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	conversation, err := sessiondomain.NewConversationSession(input.SessionID, project.ID, input.SessionTitle, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	sequence, err := conversation.AllocateRunSequence(input.StartedAt)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	run, message, err := beginRunRecords(input.RunID, conversation.ID, sequence, input.Objective, input.UserMessageID, input.UserContent, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	applyRunMetadata(&run, input.ContextFromRunID, input.Provider, input.Model, input.APIMode, input.Dialect, input.ExecutionMode)
	if err := insertConversation(ctx, tx, conversation); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	if err := insertRun(ctx, tx, run); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	if err := insertMessage(ctx, tx, message); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return sessiondomain.BeginRunResult{}, fmt.Errorf("commit first Run SQLite transaction: %w", err)
	}
	return sessiondomain.BeginRunResult{Project: project, Session: conversation, Message: message, Run: run}, nil
}

func (store *Store) BeginRun(ctx context.Context, input sessiondomain.BeginRunInput) (sessiondomain.BeginRunResult, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	tx, err := store.database.db.BeginTx(ctx, nil)
	if err != nil {
		return sessiondomain.BeginRunResult{}, fmt.Errorf("begin Run SQLite transaction: %w", err)
	}
	defer tx.Rollback()
	conversation, sequence, err := allocateRunSequence(ctx, tx, input.SessionID, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	project, err := getProjectByID(ctx, tx, conversation.ProjectID)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	if err := validateInterruptedContext(ctx, tx, input.ContextFromRunID, conversation.ID); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	messageSequence, err := nextMessageSequence(ctx, tx, conversation.ID)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	run, message, err := beginRunRecords(input.RunID, conversation.ID, sequence, input.Objective, input.UserMessageID, input.UserContent, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	message.Sequence = messageSequence
	applyRunMetadata(&run, input.ContextFromRunID, input.Provider, input.Model, input.APIMode, input.Dialect, input.ExecutionMode)
	if err := insertRun(ctx, tx, run); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	if err := insertMessage(ctx, tx, message); err != nil {
		return sessiondomain.BeginRunResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return sessiondomain.BeginRunResult{}, fmt.Errorf("commit Run SQLite transaction: %w", err)
	}
	return sessiondomain.BeginRunResult{Project: project, Session: conversation, Message: message, Run: run}, nil
}

func (store *Store) FinishRun(ctx context.Context, input sessiondomain.FinishRunInput) (sessiondomain.FinishRunResult, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.FinishRunResult{}, err
	}
	status := input.RunStatus
	completed := status == sessiondomain.RunCompleted
	if completed {
		if input.AssistantMessageID == "" || strings.TrimSpace(input.AssistantContent) == "" {
			return sessiondomain.FinishRunResult{}, errors.New("completed Run requires assistant message ID and content")
		}
	} else if input.AssistantMessageID != "" || strings.TrimSpace(input.AssistantContent) != "" {
		return sessiondomain.FinishRunResult{}, errors.New("non-completed Run cannot persist an assistant message")
	}
	tx, err := store.database.db.BeginTx(ctx, nil)
	if err != nil {
		return sessiondomain.FinishRunResult{}, fmt.Errorf("begin FinishRun SQLite transaction: %w", err)
	}
	defer tx.Rollback()
	conversation, err := getSession(ctx, tx, input.SessionID)
	if err != nil {
		return sessiondomain.FinishRunResult{}, err
	}
	run, err := getRun(ctx, tx, input.RunID)
	if err != nil || run.SessionID != conversation.ID {
		if err == nil {
			err = fmt.Errorf("%w: run %q does not belong to session %q", sessiondomain.ErrConflict, run.ID, conversation.ID)
		}
		return sessiondomain.FinishRunResult{}, err
	}
	run.UsageJSON = append(json.RawMessage(nil), input.UsageJSON...)
	run.InterruptedContextJSON = append(json.RawMessage(nil), input.InterruptedContext...)
	if err := run.Finish(status, input.StopReason, input.FinishedAt); err != nil {
		return sessiondomain.FinishRunResult{}, err
	}
	if input.FinishedAt.Before(conversation.UpdatedAt) || input.FinishedAt.Before(conversation.LastActiveAt) {
		return sessiondomain.FinishRunResult{}, errors.New("FinishRun time precedes conversation activity")
	}
	conversation.UpdatedAt = input.FinishedAt.UTC()
	conversation.LastActiveAt = input.FinishedAt.UTC()
	if err := conversation.Validate(); err != nil {
		return sessiondomain.FinishRunResult{}, err
	}
	var assistant *sessiondomain.Message
	if completed {
		sequence, err := nextMessageSequence(ctx, tx, conversation.ID)
		if err != nil {
			return sessiondomain.FinishRunResult{}, err
		}
		message, err := sessiondomain.NewMessage(input.AssistantMessageID, conversation.ID, run.ID, sequence, sessiondomain.MessageAssistant, input.AssistantContent, input.FinishedAt)
		if err != nil {
			return sessiondomain.FinishRunResult{}, err
		}
		assistant = &message
		if err := insertMessage(ctx, tx, message); err != nil {
			return sessiondomain.FinishRunResult{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE runs SET status = ?, stop_reason = ?, usage_json = ?, interrupted_context_json = ?, finished_at = ? WHERE id = ? AND status = 'running'`,
		run.Status, nullableString(run.StopReason), nullableJSON(run.UsageJSON), nullableJSON(run.InterruptedContextJSON), formatTime(*run.FinishedAt), run.ID)
	if err != nil {
		return sessiondomain.FinishRunResult{}, sqliteStoreError("finish run", err)
	}
	if err := requireOneRow(result, "finish run"); err != nil {
		return sessiondomain.FinishRunResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversation_sessions SET updated_at = ?, last_active_at = ? WHERE id = ?`, formatTime(conversation.UpdatedAt), formatTime(conversation.LastActiveAt), conversation.ID); err != nil {
		return sessiondomain.FinishRunResult{}, sqliteStoreError("update conversation activity", err)
	}
	if err := tx.Commit(); err != nil {
		return sessiondomain.FinishRunResult{}, fmt.Errorf("commit FinishRun SQLite transaction: %w", err)
	}
	return sessiondomain.FinishRunResult{Session: conversation, Run: run, AssistantMessage: assistant}, nil
}

func (store *Store) GetProjectByCanonicalPath(ctx context.Context, canonicalPath string) (sessiondomain.Project, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.Project{}, err
	}
	return scanProject(store.database.db.QueryRowContext(ctx, `SELECT id, canonical_path, display_name, created_at, updated_at, last_opened_at FROM projects WHERE canonical_path = ?`, canonicalPath))
}

func (store *Store) GetSession(ctx context.Context, id sessiondomain.ConversationSessionID) (sessiondomain.ConversationSession, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.ConversationSession{}, err
	}
	return scanSession(store.database.db.QueryRowContext(ctx, sessionSelect+` WHERE id = ?`, id))
}

func (store *Store) ListSessions(ctx context.Context, projectID sessiondomain.ProjectID) ([]sessiondomain.ConversationSession, error) {
	if err := store.validateContext(ctx); err != nil {
		return nil, err
	}
	rows, err := store.database.db.QueryContext(ctx, sessionSelect+` WHERE project_id = ? ORDER BY last_active_at DESC, id`, projectID)
	if err != nil {
		return nil, sqliteStoreError("list sessions", err)
	}
	defer rows.Close()
	result := make([]sessiondomain.ConversationSession, 0)
	for rows.Next() {
		conversation, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, conversation)
	}
	return result, sqliteStoreError("iterate sessions", rows.Err())
}

func (store *Store) LatestSession(ctx context.Context, projectID sessiondomain.ProjectID) (sessiondomain.ConversationSession, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.ConversationSession{}, err
	}
	return scanSession(store.database.db.QueryRowContext(ctx, sessionSelect+` WHERE project_id = ? AND status = 'active' ORDER BY last_active_at DESC, id LIMIT 1`, projectID))
}

func (store *Store) ListMessages(ctx context.Context, sessionID sessiondomain.ConversationSessionID) ([]sessiondomain.Message, error) {
	if err := store.validateContext(ctx); err != nil {
		return nil, err
	}
	rows, err := store.database.db.QueryContext(ctx, `SELECT id, session_id, run_id, sequence, role, content, created_at FROM conversation_messages WHERE session_id = ? ORDER BY sequence`, sessionID)
	if err != nil {
		return nil, sqliteStoreError("list conversation messages", err)
	}
	defer rows.Close()
	result := make([]sessiondomain.Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, message)
	}
	return result, sqliteStoreError("iterate conversation messages", rows.Err())
}

func (store *Store) GetRun(ctx context.Context, id sessiondomain.RunID) (sessiondomain.Run, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.Run{}, err
	}
	return scanRun(store.database.db.QueryRowContext(ctx, runSelect+` WHERE id = ?`, id))
}

func (store *Store) LatestInterruptedRun(ctx context.Context, sessionID sessiondomain.ConversationSessionID) (sessiondomain.Run, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.Run{}, err
	}
	return scanRun(store.database.db.QueryRowContext(ctx, runSelect+` WHERE session_id = ? AND status IN ('interrupted','failed') ORDER BY finished_at DESC, id LIMIT 1`, sessionID))
}

func (store *Store) PendingInterruptedRun(ctx context.Context, sessionID sessiondomain.ConversationSessionID) (sessiondomain.Run, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.Run{}, err
	}
	return scanRun(store.database.db.QueryRowContext(ctx, runSelect+` WHERE session_id = ? AND status IN ('interrupted','failed') AND interrupted_context_json IS NOT NULL
        AND finished_at > COALESCE((SELECT MAX(finished_at) FROM runs completed WHERE completed.session_id = runs.session_id AND completed.status = 'completed'), '')
        ORDER BY finished_at DESC, id LIMIT 1`, sessionID))
}

func (store *Store) RecoverRunningRuns(ctx context.Context, sessionID sessiondomain.ConversationSessionID, at time.Time) error {
	if err := store.validateContext(ctx); err != nil {
		return err
	}
	tx, err := store.database.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin recover running Runs transaction: %w", err)
	}
	defer tx.Rollback()
	conversation, err := getSession(ctx, tx, sessionID)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, runSelect+` WHERE session_id = ? AND status = 'running'`, sessionID)
	if err != nil {
		return sqliteStoreError("list running Runs", err)
	}
	var runs []sessiondomain.Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			rows.Close()
			return err
		}
		runs = append(runs, run)
	}
	if err := rows.Close(); err != nil {
		return sqliteStoreError("close running Runs", err)
	}
	if len(runs) == 0 {
		return tx.Commit()
	}
	latest := at.UTC()
	for _, run := range runs {
		finishedAt := latest
		if finishedAt.Before(run.StartedAt) {
			finishedAt = run.StartedAt
		}
		contextJSON, err := sessiondomain.EncodeInterruptedContext(sessiondomain.InterruptedContextV1{
			Objective: run.Objective, Status: string(sessiondomain.RunInterrupted), StopReason: "previous process ended before run completion",
			LastError: "previous process ended before run completion", PendingWork: []string{"Re-plan from the current workspace state."},
		})
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE runs SET status = 'interrupted', stop_reason = ?, interrupted_context_json = ?, finished_at = ? WHERE id = ? AND status = 'running'`,
			"previous process ended before run completion", string(contextJSON), formatTime(finishedAt), run.ID)
		if err != nil {
			return sqliteStoreError("recover running Run", err)
		}
		if err := requireOneRow(result, "recover running Run"); err != nil {
			return err
		}
		if finishedAt.After(latest) {
			latest = finishedAt
		}
	}
	if latest.Before(conversation.UpdatedAt) {
		latest = conversation.UpdatedAt
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversation_sessions SET updated_at = ?, last_active_at = ? WHERE id = ?`, formatTime(latest), formatTime(latest), sessionID); err != nil {
		return sqliteStoreError("update recovered conversation activity", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit recover running Runs transaction: %w", err)
	}
	return nil
}

func (store *Store) AppendSummary(ctx context.Context, summary sessiondomain.ConversationSummary) (sessiondomain.ConversationSummary, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.ConversationSummary{}, err
	}
	if err := summary.Validate(); err != nil {
		return sessiondomain.ConversationSummary{}, err
	}
	_, err := store.database.db.ExecContext(ctx, `INSERT INTO conversation_summaries (
        id, session_id, from_message_sequence, to_message_sequence, content, source_hash, summary_hash, provider, model, created_at
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, summary.ID, summary.SessionID, summary.FromMessageSequence, summary.ToMessageSequence,
		summary.Content, summary.SourceHash, summary.SummaryHash, nullableString(summary.Provider), nullableString(summary.Model), formatTime(summary.CreatedAt))
	if err != nil {
		return sessiondomain.ConversationSummary{}, sqliteStoreError("append conversation summary", err)
	}
	return summary, nil
}

func (store *Store) LatestSummary(ctx context.Context, sessionID sessiondomain.ConversationSessionID) (sessiondomain.ConversationSummary, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.ConversationSummary{}, err
	}
	return scanSummary(store.database.db.QueryRowContext(ctx, `SELECT id, session_id, from_message_sequence, to_message_sequence,
        content, source_hash, summary_hash, provider, model, created_at FROM conversation_summaries
        WHERE session_id = ? ORDER BY to_message_sequence DESC, from_message_sequence DESC, created_at DESC LIMIT 1`, sessionID))
}

func beginRunRecords(runID sessiondomain.RunID, sessionID sessiondomain.ConversationSessionID, sequence int64, objective string, messageID sessiondomain.MessageID, content string, startedAt time.Time) (sessiondomain.Run, sessiondomain.Message, error) {
	run, err := sessiondomain.NewSequencedRun(runID, sessionID, sequence, objective, startedAt)
	if err != nil {
		return sessiondomain.Run{}, sessiondomain.Message{}, err
	}
	message, err := sessiondomain.NewMessage(messageID, sessionID, runID, 1, sessiondomain.MessageUser, content, startedAt)
	return run, message, err
}

func applyRunMetadata(run *sessiondomain.Run, contextID sessiondomain.RunID, provider, model, apiMode, dialect string, executionMode sessiondomain.ExecutionMode) {
	run.ContextFromRunID = contextID
	run.Provider = strings.TrimSpace(provider)
	run.Model = strings.TrimSpace(model)
	run.APIMode = strings.TrimSpace(apiMode)
	run.Dialect = strings.TrimSpace(dialect)
	if executionMode.Valid() {
		run.ExecutionMode = executionMode
	}
}

func upsertProject(ctx context.Context, tx *sql.Tx, candidate sessiondomain.Project, at time.Time) (sessiondomain.Project, error) {
	_, err := tx.ExecContext(ctx, `INSERT INTO projects(id, canonical_path, display_name, created_at, updated_at, last_opened_at)
        VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(canonical_path) DO UPDATE SET display_name = excluded.display_name,
        updated_at = excluded.updated_at, last_opened_at = excluded.last_opened_at`, candidate.ID, candidate.CanonicalPath, candidate.DisplayName,
		formatTime(candidate.CreatedAt), formatTime(at), formatTime(at))
	if err != nil {
		return sessiondomain.Project{}, sqliteStoreError("upsert project", err)
	}
	return scanProject(tx.QueryRowContext(ctx, `SELECT id, canonical_path, display_name, created_at, updated_at, last_opened_at FROM projects WHERE canonical_path = ?`, candidate.CanonicalPath))
}

func insertConversation(ctx context.Context, tx *sql.Tx, conversation sessiondomain.ConversationSession) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO conversation_sessions(id, project_id, title, status, next_run_sequence, created_at, updated_at, last_active_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		conversation.ID, conversation.ProjectID, conversation.Title, conversation.Status, conversation.NextRunSequence,
		formatTime(conversation.CreatedAt), formatTime(conversation.UpdatedAt), formatTime(conversation.LastActiveAt))
	return sqliteStoreError("insert conversation session", err)
}

func insertRun(ctx context.Context, tx *sql.Tx, run sessiondomain.Run) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO runs(id, session_id, sequence, context_from_run_id, objective, status, stop_reason,
		provider, model, api_mode, dialect, execution_mode, usage_json, interrupted_context_json, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, NULL, NULL, ?, NULL)`, run.ID, run.SessionID, run.Sequence, nullableRunID(run.ContextFromRunID),
		run.Objective, run.Status, nullableString(run.Provider), nullableString(run.Model), nullableString(run.APIMode), nullableString(run.Dialect), run.ExecutionMode, formatTime(run.StartedAt))
	return sqliteStoreError("insert run", err)
}

func insertMessage(ctx context.Context, tx *sql.Tx, message sessiondomain.Message) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO conversation_messages(id, session_id, run_id, sequence, role, content, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		message.ID, message.SessionID, message.RunID, message.Sequence, message.Role, message.Content, formatTime(message.CreatedAt))
	return sqliteStoreError("insert conversation message", err)
}

func allocateRunSequence(ctx context.Context, tx *sql.Tx, sessionID sessiondomain.ConversationSessionID, at time.Time) (sessiondomain.ConversationSession, int64, error) {
	conversation, err := scanSession(tx.QueryRowContext(ctx, `UPDATE conversation_sessions SET next_run_sequence = next_run_sequence + 1,
        updated_at = ?, last_active_at = ? WHERE id = ? AND status = 'active' AND updated_at <= ? AND last_active_at <= ?
	        RETURNING id, project_id, title, status, next_run_sequence, created_at, updated_at, last_active_at`,
		formatTime(at), formatTime(at), sessionID, formatTime(at), formatTime(at)))
	if err != nil {
		return sessiondomain.ConversationSession{}, 0, err
	}
	return conversation, conversation.NextRunSequence - 1, nil
}

func nextMessageSequence(ctx context.Context, tx *sql.Tx, sessionID sessiondomain.ConversationSessionID) (int64, error) {
	var sequence int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM conversation_messages WHERE session_id = ?`, sessionID).Scan(&sequence); err != nil {
		return 0, sqliteStoreError("allocate conversation message sequence", err)
	}
	return sequence, nil
}

func validateInterruptedContext(ctx context.Context, tx *sql.Tx, runID sessiondomain.RunID, sessionID sessiondomain.ConversationSessionID) error {
	if runID == "" {
		return nil
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE id = ? AND session_id = ? AND status IN ('interrupted','failed')`, runID, sessionID).Scan(&count); err != nil {
		return sqliteStoreError("validate interrupted context run", err)
	}
	if count != 1 {
		return fmt.Errorf("%w: interrupted context run %q", sessiondomain.ErrConflict, runID)
	}
	return nil
}

func getProjectByID(ctx context.Context, tx *sql.Tx, id sessiondomain.ProjectID) (sessiondomain.Project, error) {
	return scanProject(tx.QueryRowContext(ctx, `SELECT id, canonical_path, display_name, created_at, updated_at, last_opened_at FROM projects WHERE id = ?`, id))
}

func getSession(ctx context.Context, tx *sql.Tx, id sessiondomain.ConversationSessionID) (sessiondomain.ConversationSession, error) {
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

var (
	_ sessiondomain.SessionStore      = (*Store)(nil)
	_ sessiondomain.ConversationStore = (*Store)(nil)
	_ sessiondomain.RunStore          = (*Store)(nil)
	_ sessiondomain.SummaryStore      = (*Store)(nil)
)
