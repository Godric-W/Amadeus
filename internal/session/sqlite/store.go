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

func (store *Store) BeginFirstTurn(ctx context.Context, input sessiondomain.BeginFirstTurnInput) (sessiondomain.BeginTurnResult, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	if input.ContextFromRunID != "" {
		return sessiondomain.BeginTurnResult{}, fmt.Errorf("%w: a new conversation session cannot reference an interrupted run", sessiondomain.ErrConflict)
	}
	candidateProject, err := sessiondomain.NewProject(input.ProjectID, input.CanonicalPath, input.ProjectName, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	tx, err := store.database.db.BeginTx(ctx, nil)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, fmt.Errorf("begin first Turn SQLite transaction: %w", err)
	}
	defer tx.Rollback()

	project, err := upsertProject(ctx, tx, candidateProject, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	conversation, err := sessiondomain.NewConversationSession(input.SessionID, project.ID, input.SessionTitle, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	turnSequence, err := conversation.AllocateTurnSequence(input.StartedAt)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	turn, err := sessiondomain.NewTurn(input.TurnID, conversation.ID, turnSequence, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	message, err := sessiondomain.NewMessage(input.UserMessageID, conversation.ID, turn.ID, 1, sessiondomain.MessageUser, input.UserContent, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	run, err := newPersistentRun(firstRunInput(input), conversation.ID, turn.ID)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	if err := insertConversation(ctx, tx, conversation); err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	if err := insertTurn(ctx, tx, turn); err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	if err := insertMessage(ctx, tx, message); err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	if err := insertRun(ctx, tx, run); err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return sessiondomain.BeginTurnResult{}, fmt.Errorf("commit first Turn SQLite transaction: %w", err)
	}
	return sessiondomain.BeginTurnResult{Project: project, Session: conversation, Turn: turn, Message: message, Run: run}, nil
}

func (store *Store) BeginTurn(ctx context.Context, input sessiondomain.BeginTurnInput) (sessiondomain.BeginTurnResult, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	tx, err := store.database.db.BeginTx(ctx, nil)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, fmt.Errorf("begin Turn SQLite transaction: %w", err)
	}
	defer tx.Rollback()

	conversation, sequence, err := allocateTurnSequence(ctx, tx, input.SessionID, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	project, err := getProjectByID(ctx, tx, conversation.ProjectID)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	if err := validateInterruptedContext(ctx, tx, input.ContextFromRunID, conversation.ID); err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	turn, err := sessiondomain.NewTurn(input.TurnID, conversation.ID, sequence, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	messageSequence, err := nextMessageSequence(ctx, tx, conversation.ID)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	message, err := sessiondomain.NewMessage(input.UserMessageID, conversation.ID, turn.ID, messageSequence, sessiondomain.MessageUser, input.UserContent, input.StartedAt)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	run, err := newPersistentRun(nextRunInput(input), conversation.ID, turn.ID)
	if err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	if err := insertTurn(ctx, tx, turn); err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	if err := insertMessage(ctx, tx, message); err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	if err := insertRun(ctx, tx, run); err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE projects SET updated_at = ?, last_opened_at = ? WHERE id = ?`,
		formatTime(input.StartedAt), formatTime(input.StartedAt), project.ID,
	); err != nil {
		return sessiondomain.BeginTurnResult{}, sqliteStoreError("update project activity", err)
	}
	project.UpdatedAt = input.StartedAt.UTC()
	project.LastOpenedAt = input.StartedAt.UTC()
	if err := project.Validate(); err != nil {
		return sessiondomain.BeginTurnResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return sessiondomain.BeginTurnResult{}, fmt.Errorf("commit Turn SQLite transaction: %w", err)
	}
	return sessiondomain.BeginTurnResult{Project: project, Session: conversation, Turn: turn, Message: message, Run: run}, nil
}

func (store *Store) FinishTurn(ctx context.Context, input sessiondomain.FinishTurnInput) (sessiondomain.FinishTurnResult, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.FinishTurnResult{}, err
	}
	if !matchingStatuses(input.TurnStatus, input.RunStatus) {
		return sessiondomain.FinishTurnResult{}, fmt.Errorf("%w: turn status %q does not match run status %q", sessiondomain.ErrConflict, input.TurnStatus, input.RunStatus)
	}
	completed := input.TurnStatus == sessiondomain.TurnCompleted
	if completed {
		if strings.TrimSpace(string(input.AssistantMessageID)) == "" || strings.TrimSpace(input.AssistantContent) == "" {
			return sessiondomain.FinishTurnResult{}, errors.New("completed turn requires assistant message ID and content")
		}
	} else if input.AssistantMessageID != "" || strings.TrimSpace(input.AssistantContent) != "" {
		return sessiondomain.FinishTurnResult{}, errors.New("non-completed turn cannot persist an assistant message")
	}
	tx, err := store.database.db.BeginTx(ctx, nil)
	if err != nil {
		return sessiondomain.FinishTurnResult{}, fmt.Errorf("begin FinishTurn SQLite transaction: %w", err)
	}
	defer tx.Rollback()
	conversation, err := getSession(ctx, tx, input.SessionID)
	if err != nil {
		return sessiondomain.FinishTurnResult{}, err
	}
	turn, err := getTurn(ctx, tx, input.TurnID)
	if err != nil || turn.SessionID != conversation.ID {
		if err == nil {
			err = fmt.Errorf("%w: turn %q does not belong to session %q", sessiondomain.ErrConflict, turn.ID, conversation.ID)
		}
		return sessiondomain.FinishTurnResult{}, err
	}
	run, err := getRun(ctx, tx, input.RunID)
	if err != nil || run.SessionID != conversation.ID || run.TurnID != turn.ID {
		if err == nil {
			err = fmt.Errorf("%w: run %q does not belong to turn %q", sessiondomain.ErrConflict, run.ID, turn.ID)
		}
		return sessiondomain.FinishTurnResult{}, err
	}
	run.UsageJSON = append(json.RawMessage(nil), input.UsageJSON...)
	if err := run.Finish(input.RunStatus, input.StopReason, input.FinishedAt); err != nil {
		return sessiondomain.FinishTurnResult{}, err
	}
	if err := turn.Complete(input.TurnStatus, input.FinishedAt); err != nil {
		return sessiondomain.FinishTurnResult{}, err
	}
	if input.FinishedAt.Before(conversation.UpdatedAt) || input.FinishedAt.Before(conversation.LastActiveAt) {
		return sessiondomain.FinishTurnResult{}, errors.New("FinishTurn time precedes conversation activity")
	}
	conversation.UpdatedAt = input.FinishedAt.UTC()
	conversation.LastActiveAt = input.FinishedAt.UTC()
	if err := conversation.Validate(); err != nil {
		return sessiondomain.FinishTurnResult{}, err
	}

	var assistant *sessiondomain.Message
	if completed {
		sequence, err := nextMessageSequence(ctx, tx, conversation.ID)
		if err != nil {
			return sessiondomain.FinishTurnResult{}, err
		}
		message, err := sessiondomain.NewMessage(input.AssistantMessageID, conversation.ID, turn.ID, sequence, sessiondomain.MessageAssistant, input.AssistantContent, input.FinishedAt)
		if err != nil {
			return sessiondomain.FinishTurnResult{}, err
		}
		assistant = &message
		if err := insertMessage(ctx, tx, message); err != nil {
			return sessiondomain.FinishTurnResult{}, err
		}
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE runs SET status = ?, stop_reason = ?, usage_json = ?, finished_at = ? WHERE id = ? AND status = 'running'`,
		run.Status, nullableString(run.StopReason), nullableJSON(run.UsageJSON), formatTime(*run.FinishedAt), run.ID,
	)
	if err != nil {
		return sessiondomain.FinishTurnResult{}, sqliteStoreError("finish run", err)
	}
	if err := requireOneRow(result, "finish run"); err != nil {
		return sessiondomain.FinishTurnResult{}, err
	}
	result, err = tx.ExecContext(ctx,
		`UPDATE session_turns SET status = ?, completed_at = ? WHERE id = ? AND status = 'running'`,
		turn.Status, formatTime(*turn.CompletedAt), turn.ID,
	)
	if err != nil {
		return sessiondomain.FinishTurnResult{}, sqliteStoreError("finish turn", err)
	}
	if err := requireOneRow(result, "finish turn"); err != nil {
		return sessiondomain.FinishTurnResult{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE conversation_sessions SET updated_at = ?, last_active_at = ? WHERE id = ?`,
		formatTime(conversation.UpdatedAt), formatTime(conversation.LastActiveAt), conversation.ID,
	); err != nil {
		return sessiondomain.FinishTurnResult{}, sqliteStoreError("update conversation activity", err)
	}
	if err := tx.Commit(); err != nil {
		return sessiondomain.FinishTurnResult{}, fmt.Errorf("commit FinishTurn SQLite transaction: %w", err)
	}
	return sessiondomain.FinishTurnResult{Session: conversation, Turn: turn, Run: run, AssistantMessage: assistant}, nil
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
	if err := rows.Err(); err != nil {
		return nil, sqliteStoreError("iterate sessions", err)
	}
	return result, nil
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
	rows, err := store.database.db.QueryContext(ctx,
		`SELECT id, session_id, turn_id, sequence, role, content, created_at FROM conversation_messages WHERE session_id = ? ORDER BY sequence`, sessionID,
	)
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
	if err := rows.Err(); err != nil {
		return nil, sqliteStoreError("iterate conversation messages", err)
	}
	return result, nil
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
	return scanRun(store.database.db.QueryRowContext(ctx, runSelect+` WHERE session_id = ? AND status = 'cancelled' ORDER BY finished_at DESC, id LIMIT 1`, sessionID))
}

func (store *Store) PendingInterruptedRun(ctx context.Context, sessionID sessiondomain.ConversationSessionID) (sessiondomain.Run, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.Run{}, err
	}
	return scanRun(store.database.db.QueryRowContext(ctx, runSelect+` WHERE session_id = ? AND status = 'cancelled'
		AND finished_at > COALESCE((SELECT MAX(finished_at) FROM runs completed WHERE completed.session_id = runs.session_id AND completed.status = 'completed'), '')
		ORDER BY finished_at DESC, id LIMIT 1`, sessionID))
}

func (store *Store) AppendCheckpoint(ctx context.Context, input sessiondomain.AppendCheckpointInput) (sessiondomain.Checkpoint, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.Checkpoint{}, err
	}
	if err := input.Checkpoint.VerifyPayload(); err != nil {
		return sessiondomain.Checkpoint{}, err
	}
	seen := make(map[int]struct{}, len(input.Instructions))
	for _, instruction := range input.Instructions {
		if instruction.CheckpointID != input.Checkpoint.ID {
			return sessiondomain.Checkpoint{}, errors.New("checkpoint instruction references a different checkpoint")
		}
		if err := instruction.Validate(); err != nil {
			return sessiondomain.Checkpoint{}, err
		}
		if _, duplicate := seen[instruction.Precedence]; duplicate {
			return sessiondomain.Checkpoint{}, fmt.Errorf("%w: checkpoint instruction precedence %d", sessiondomain.ErrConflict, instruction.Precedence)
		}
		seen[instruction.Precedence] = struct{}{}
	}
	tx, err := store.database.db.BeginTx(ctx, nil)
	if err != nil {
		return sessiondomain.Checkpoint{}, fmt.Errorf("begin checkpoint SQLite transaction: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx,
		`UPDATE runs SET latest_checkpoint_seq = ? WHERE id = ? AND latest_checkpoint_seq = ?`,
		input.Checkpoint.Sequence, input.Checkpoint.RunID, input.Checkpoint.Sequence-1,
	)
	if err != nil {
		return sessiondomain.Checkpoint{}, sqliteStoreError("advance run checkpoint sequence", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return sessiondomain.Checkpoint{}, sqliteStoreError("read run checkpoint update", err)
	}
	if rows != 1 {
		var exists int
		if queryErr := tx.QueryRowContext(ctx, `SELECT 1 FROM runs WHERE id = ?`, input.Checkpoint.RunID).Scan(&exists); errors.Is(queryErr, sql.ErrNoRows) {
			return sessiondomain.Checkpoint{}, fmt.Errorf("%w: run %q", sessiondomain.ErrNotFound, input.Checkpoint.RunID)
		}
		return sessiondomain.Checkpoint{}, fmt.Errorf("%w: checkpoint sequence %d is not next", sessiondomain.ErrConflict, input.Checkpoint.Sequence)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO run_checkpoints(id, run_id, sequence, schema_version, reason, payload_json, payload_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, input.Checkpoint.ID, input.Checkpoint.RunID, input.Checkpoint.Sequence,
		input.Checkpoint.SchemaVersion, input.Checkpoint.Reason, string(input.Checkpoint.PayloadJSON), input.Checkpoint.PayloadHash,
		formatTime(input.Checkpoint.CreatedAt)); err != nil {
		return sessiondomain.Checkpoint{}, sqliteStoreError("insert run checkpoint", err)
	}
	for _, instruction := range input.Instructions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO checkpoint_instructions(checkpoint_id, precedence, path, scope_path, content_hash)
			VALUES (?, ?, ?, ?, ?)`, instruction.CheckpointID, instruction.Precedence, instruction.Path, instruction.ScopePath, instruction.ContentHash); err != nil {
			return sessiondomain.Checkpoint{}, sqliteStoreError("insert checkpoint instruction", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return sessiondomain.Checkpoint{}, fmt.Errorf("commit checkpoint SQLite transaction: %w", err)
	}
	return input.Checkpoint, nil
}

func (store *Store) ListCheckpoints(ctx context.Context, runID sessiondomain.RunID) ([]sessiondomain.Checkpoint, error) {
	if err := store.validateContext(ctx); err != nil {
		return nil, err
	}
	rows, err := store.database.db.QueryContext(ctx, `SELECT id, run_id, sequence, schema_version, reason, payload_json, payload_hash, created_at
		FROM run_checkpoints WHERE run_id = ? ORDER BY sequence`, runID)
	if err != nil {
		return nil, sqliteStoreError("list run checkpoints", err)
	}
	defer rows.Close()
	checkpoints := make([]sessiondomain.Checkpoint, 0)
	for rows.Next() {
		checkpoint, err := scanCheckpoint(rows)
		if err != nil {
			return nil, err
		}
		checkpoints = append(checkpoints, checkpoint)
	}
	if err := rows.Err(); err != nil {
		return nil, sqliteStoreError("iterate run checkpoints", err)
	}
	if len(checkpoints) == 0 {
		var exists int
		if err := store.database.db.QueryRowContext(ctx, `SELECT 1 FROM runs WHERE id = ?`, runID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: run %q", sessiondomain.ErrNotFound, runID)
		} else if err != nil {
			return nil, sqliteStoreError("check run for checkpoints", err)
		}
	}
	return checkpoints, nil
}

func (store *Store) ListCheckpointInstructions(ctx context.Context, checkpointID sessiondomain.CheckpointID) ([]sessiondomain.CheckpointInstruction, error) {
	if err := store.validateContext(ctx); err != nil {
		return nil, err
	}
	rows, err := store.database.db.QueryContext(ctx, `SELECT checkpoint_id, precedence, path, scope_path, content_hash
		FROM checkpoint_instructions WHERE checkpoint_id = ? ORDER BY precedence`, checkpointID)
	if err != nil {
		return nil, sqliteStoreError("list checkpoint instructions", err)
	}
	defer rows.Close()
	result := make([]sessiondomain.CheckpointInstruction, 0)
	for rows.Next() {
		var instruction sessiondomain.CheckpointInstruction
		if err := rows.Scan(&instruction.CheckpointID, &instruction.Precedence, &instruction.Path, &instruction.ScopePath, &instruction.ContentHash); err != nil {
			return nil, sqliteStoreError("scan checkpoint instruction", err)
		}
		if err := instruction.Validate(); err != nil {
			return nil, err
		}
		result = append(result, instruction)
	}
	if err := rows.Err(); err != nil {
		return nil, sqliteStoreError("iterate checkpoint instructions", err)
	}
	if len(result) == 0 {
		var exists int
		if err := store.database.db.QueryRowContext(ctx, `SELECT 1 FROM run_checkpoints WHERE id = ?`, checkpointID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: checkpoint %q", sessiondomain.ErrNotFound, checkpointID)
		} else if err != nil {
			return nil, sqliteStoreError("check checkpoint for instructions", err)
		}
	}
	return result, nil
}

func (store *Store) AppendSummary(ctx context.Context, summary sessiondomain.ConversationSummary) (sessiondomain.ConversationSummary, error) {
	if err := store.validateContext(ctx); err != nil {
		return sessiondomain.ConversationSummary{}, err
	}
	if err := summary.Validate(); err != nil {
		return sessiondomain.ConversationSummary{}, err
	}
	if existing, err := scanSummary(store.database.db.QueryRowContext(ctx, `SELECT id, session_id, from_message_sequence, to_message_sequence,
		content, source_hash, summary_hash, provider, model, created_at FROM conversation_summaries
		WHERE session_id = ? AND from_message_sequence = ? AND to_message_sequence = ? AND source_hash = ? LIMIT 1`,
		summary.SessionID, summary.FromMessageSequence, summary.ToMessageSequence, summary.SourceHash)); err == nil {
		return existing, nil
	} else if !errors.Is(err, sessiondomain.ErrNotFound) {
		return sessiondomain.ConversationSummary{}, err
	}
	_, err := store.database.db.ExecContext(ctx, `INSERT INTO conversation_summaries(id, session_id, from_message_sequence, to_message_sequence,
		content, source_hash, summary_hash, provider, model, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		summary.ID, summary.SessionID, summary.FromMessageSequence, summary.ToMessageSequence, summary.Content,
		summary.SourceHash, summary.SummaryHash, nullableString(summary.Provider), nullableString(summary.Model), formatTime(summary.CreatedAt))
	if err != nil {
		return sessiondomain.ConversationSummary{}, sqliteStoreError("insert conversation summary", err)
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

func (store *Store) validateContext(ctx context.Context) error {
	if store == nil || store.database == nil || store.database.db == nil {
		return errors.New("SQLite session store is nil")
	}
	if ctx == nil {
		return errors.New("SQLite session store context is nil")
	}
	return ctx.Err()
}

type runInput struct {
	ID               sessiondomain.RunID
	Objective        string
	ContextFromRunID sessiondomain.RunID
	Provider         string
	Model            string
	APIMode          string
	Dialect          string
	BudgetJSON       json.RawMessage
	StartedAt        time.Time
}

var _ sessiondomain.Store = (*Store)(nil)

func firstRunInput(input sessiondomain.BeginFirstTurnInput) runInput {
	return runInput{ID: input.RunID, Objective: input.Objective, ContextFromRunID: input.ContextFromRunID, Provider: input.Provider, Model: input.Model, APIMode: input.APIMode, Dialect: input.Dialect, BudgetJSON: input.BudgetJSON, StartedAt: input.StartedAt}
}

func nextRunInput(input sessiondomain.BeginTurnInput) runInput {
	return runInput{ID: input.RunID, Objective: input.Objective, ContextFromRunID: input.ContextFromRunID, Provider: input.Provider, Model: input.Model, APIMode: input.APIMode, Dialect: input.Dialect, BudgetJSON: input.BudgetJSON, StartedAt: input.StartedAt}
}

func newPersistentRun(input runInput, sessionID sessiondomain.ConversationSessionID, turnID sessiondomain.TurnID) (sessiondomain.Run, error) {
	run, err := sessiondomain.NewRun(input.ID, sessionID, turnID, input.Objective, input.StartedAt)
	if err != nil {
		return sessiondomain.Run{}, err
	}
	run.ContextFromRunID = input.ContextFromRunID
	run.Provider = strings.TrimSpace(input.Provider)
	run.Model = strings.TrimSpace(input.Model)
	run.APIMode = strings.TrimSpace(input.APIMode)
	run.Dialect = strings.TrimSpace(input.Dialect)
	run.BudgetJSON = append(json.RawMessage(nil), input.BudgetJSON...)
	return run, run.Validate()
}

func upsertProject(ctx context.Context, tx *sql.Tx, project sessiondomain.Project, openedAt time.Time) (sessiondomain.Project, error) {
	return scanProject(tx.QueryRowContext(ctx, `INSERT INTO projects(id, canonical_path, display_name, created_at, updated_at, last_opened_at)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT(canonical_path) DO UPDATE SET
            display_name = excluded.display_name,
            updated_at = excluded.updated_at,
            last_opened_at = excluded.last_opened_at
        RETURNING id, canonical_path, display_name, created_at, updated_at, last_opened_at`,
		project.ID, project.CanonicalPath, project.DisplayName, formatTime(project.CreatedAt), formatTime(openedAt), formatTime(openedAt),
	))
}

func insertConversation(ctx context.Context, tx *sql.Tx, conversation sessiondomain.ConversationSession) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO conversation_sessions(
        id, project_id, title, status, next_turn_sequence, created_at, updated_at, last_active_at
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		conversation.ID, conversation.ProjectID, conversation.Title, conversation.Status, conversation.NextTurnSequence,
		formatTime(conversation.CreatedAt), formatTime(conversation.UpdatedAt), formatTime(conversation.LastActiveAt),
	)
	return sqliteStoreError("insert conversation session", err)
}

func insertTurn(ctx context.Context, tx *sql.Tx, turn sessiondomain.Turn) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO session_turns(id, session_id, sequence, status, created_at, completed_at) VALUES (?, ?, ?, ?, ?, NULL)`, turn.ID, turn.SessionID, turn.Sequence, turn.Status, formatTime(turn.CreatedAt))
	return sqliteStoreError("insert turn", err)
}

func insertMessage(ctx context.Context, tx *sql.Tx, message sessiondomain.Message) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO conversation_messages(id, session_id, turn_id, sequence, role, content, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, message.ID, message.SessionID, message.TurnID, message.Sequence, message.Role, message.Content, formatTime(message.CreatedAt))
	return sqliteStoreError("insert conversation message", err)
}

func insertRun(ctx context.Context, tx *sql.Tx, run sessiondomain.Run) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO runs(
        id, session_id, turn_id, context_from_run_id, objective, status, stop_reason,
        provider, model, api_mode, dialect, budget_json, usage_json, latest_checkpoint_seq, started_at, finished_at
    ) VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, NULL, 0, ?, NULL)`,
		run.ID, run.SessionID, run.TurnID, nullableRunID(run.ContextFromRunID), run.Objective, run.Status,
		nullableString(run.Provider), nullableString(run.Model), nullableString(run.APIMode), nullableString(run.Dialect), nullableJSON(run.BudgetJSON), formatTime(run.StartedAt),
	)
	return sqliteStoreError("insert run", err)
}

func allocateTurnSequence(ctx context.Context, tx *sql.Tx, sessionID sessiondomain.ConversationSessionID, at time.Time) (sessiondomain.ConversationSession, int64, error) {
	conversation, err := scanSession(tx.QueryRowContext(ctx, `UPDATE conversation_sessions
        SET next_turn_sequence = next_turn_sequence + 1, updated_at = ?, last_active_at = ?
        WHERE id = ? AND status = 'active' AND updated_at <= ? AND last_active_at <= ?
        RETURNING id, project_id, title, status, next_turn_sequence, created_at, updated_at, last_active_at`,
		formatTime(at), formatTime(at), sessionID, formatTime(at), formatTime(at),
	))
	if err != nil {
		return sessiondomain.ConversationSession{}, 0, err
	}
	return conversation, conversation.NextTurnSequence - 1, nil
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
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE id = ? AND session_id = ? AND status = 'cancelled'`, runID, sessionID).Scan(&count); err != nil {
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

func getTurn(ctx context.Context, tx *sql.Tx, id sessiondomain.TurnID) (sessiondomain.Turn, error) {
	return scanTurn(tx.QueryRowContext(ctx, `SELECT id, session_id, sequence, status, created_at, completed_at FROM session_turns WHERE id = ?`, id))
}

func getRun(ctx context.Context, tx *sql.Tx, id sessiondomain.RunID) (sessiondomain.Run, error) {
	return scanRun(tx.QueryRowContext(ctx, runSelect+` WHERE id = ?`, id))
}

func matchingStatuses(turn sessiondomain.TurnStatus, run sessiondomain.RunStatus) bool {
	return (turn == sessiondomain.TurnCompleted && run == sessiondomain.RunCompleted) ||
		(turn == sessiondomain.TurnCancelled && run == sessiondomain.RunCancelled) ||
		(turn == sessiondomain.TurnFailed && run == sessiondomain.RunFailed) ||
		(turn == sessiondomain.TurnPartial && run == sessiondomain.RunPartial) ||
		(turn == sessiondomain.TurnNeedsPlan && run == sessiondomain.RunNeedsPlan) ||
		(turn == sessiondomain.TurnAwaitingUser && run == sessiondomain.RunAwaitingUser)
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
)
