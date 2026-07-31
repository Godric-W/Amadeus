package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrUnknownSchemaVersion = errors.New("SQLite schema contains an unknown migration version")
	ErrMigrationDrift       = errors.New("SQLite schema migration history drifted")
)

type migration struct {
	version    int
	name       string
	statements []string
}

var schemaMigrations = []migration{{
	version: 1,
	name:    "initial_session_schema",
	statements: []string{
		`CREATE TABLE projects (
            id TEXT PRIMARY KEY,
            canonical_path TEXT NOT NULL UNIQUE,
            display_name TEXT NOT NULL,
            created_at TEXT NOT NULL,
            updated_at TEXT NOT NULL,
            last_opened_at TEXT NOT NULL,
            CHECK (length(trim(id)) > 0),
            CHECK (length(trim(canonical_path)) > 0),
            CHECK (length(trim(display_name)) > 0)
        )`,
		`CREATE TABLE conversation_sessions (
            id TEXT PRIMARY KEY,
            project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
            title TEXT NOT NULL,
            status TEXT NOT NULL CHECK (status IN ('active', 'archived')),
            next_turn_sequence INTEGER NOT NULL CHECK (next_turn_sequence >= 1),
            created_at TEXT NOT NULL,
            updated_at TEXT NOT NULL,
            last_active_at TEXT NOT NULL,
            CHECK (length(trim(id)) > 0),
            CHECK (length(trim(title)) > 0)
        )`,
		`CREATE INDEX conversation_sessions_project_active_idx
            ON conversation_sessions(project_id, status, last_active_at DESC, id)`,
		`CREATE TABLE session_turns (
            id TEXT PRIMARY KEY,
            session_id TEXT NOT NULL REFERENCES conversation_sessions(id) ON DELETE CASCADE,
            sequence INTEGER NOT NULL CHECK (sequence >= 1),
            status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'cancelled', 'failed', 'partial', 'needs_plan', 'awaiting_user')),
            created_at TEXT NOT NULL,
            completed_at TEXT,
            UNIQUE (session_id, sequence),
            CHECK (length(trim(id)) > 0),
            CHECK ((status = 'running' AND completed_at IS NULL) OR (status <> 'running' AND completed_at IS NOT NULL))
        )`,
		`CREATE INDEX session_turns_session_sequence_idx ON session_turns(session_id, sequence)`,
		`CREATE TABLE conversation_messages (
            id TEXT PRIMARY KEY,
            session_id TEXT NOT NULL REFERENCES conversation_sessions(id) ON DELETE CASCADE,
            turn_id TEXT NOT NULL REFERENCES session_turns(id) ON DELETE CASCADE,
            sequence INTEGER NOT NULL CHECK (sequence >= 1),
            role TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
            content TEXT NOT NULL,
            created_at TEXT NOT NULL,
            UNIQUE (session_id, sequence),
            UNIQUE (turn_id, role),
            CHECK (length(trim(id)) > 0),
            CHECK (length(trim(content)) > 0)
        )`,
		`CREATE INDEX conversation_messages_session_sequence_idx ON conversation_messages(session_id, sequence)`,
		`CREATE TABLE conversation_summaries (
            id TEXT PRIMARY KEY,
            session_id TEXT NOT NULL REFERENCES conversation_sessions(id) ON DELETE CASCADE,
            from_message_sequence INTEGER NOT NULL CHECK (from_message_sequence >= 1),
            to_message_sequence INTEGER NOT NULL CHECK (to_message_sequence >= from_message_sequence),
            content TEXT NOT NULL,
            source_hash TEXT NOT NULL,
            summary_hash TEXT NOT NULL,
            provider TEXT,
            model TEXT,
            created_at TEXT NOT NULL,
            CHECK (length(trim(id)) > 0),
            CHECK (length(trim(content)) > 0),
            CHECK (length(source_hash) = 64),
            CHECK (length(summary_hash) = 64)
        )`,
		`CREATE INDEX conversation_summaries_session_range_idx
            ON conversation_summaries(session_id, to_message_sequence DESC, from_message_sequence DESC)`,
		`CREATE TABLE runs (
            id TEXT PRIMARY KEY,
            session_id TEXT NOT NULL REFERENCES conversation_sessions(id) ON DELETE CASCADE,
            turn_id TEXT NOT NULL UNIQUE REFERENCES session_turns(id) ON DELETE CASCADE,
            context_from_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
            objective TEXT NOT NULL,
            status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'cancelled', 'failed', 'partial', 'needs_plan', 'awaiting_user')),
            stop_reason TEXT,
            provider TEXT,
            model TEXT,
            api_mode TEXT,
            dialect TEXT,
            budget_json TEXT,
            usage_json TEXT,
            latest_checkpoint_seq INTEGER NOT NULL DEFAULT 0 CHECK (latest_checkpoint_seq >= 0),
            started_at TEXT NOT NULL,
            finished_at TEXT,
            CHECK (length(trim(id)) > 0),
            CHECK (length(trim(objective)) > 0),
            CHECK (context_from_run_id IS NULL OR context_from_run_id <> id),
            CHECK ((status = 'running' AND finished_at IS NULL) OR (status <> 'running' AND finished_at IS NOT NULL)),
            CHECK (status = 'completed' OR status = 'running' OR length(trim(stop_reason)) > 0),
            CHECK (budget_json IS NULL OR json_valid(budget_json)),
            CHECK (usage_json IS NULL OR json_valid(usage_json))
        )`,
		`CREATE INDEX runs_session_status_finished_idx ON runs(session_id, status, finished_at DESC, id)`,
		`CREATE INDEX runs_context_from_idx ON runs(context_from_run_id)`,
		`CREATE TABLE run_checkpoints (
            id TEXT PRIMARY KEY,
            run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
            sequence INTEGER NOT NULL CHECK (sequence >= 1),
            schema_version INTEGER NOT NULL CHECK (schema_version >= 1),
            reason TEXT NOT NULL CHECK (reason IN ('run_started', 'tool_completed', 'step_completed', 'user_cancelled', 'run_completed', 'run_failed')),
            payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
            payload_hash TEXT NOT NULL CHECK (length(payload_hash) = 64),
            created_at TEXT NOT NULL,
            UNIQUE (run_id, sequence),
            CHECK (length(trim(id)) > 0)
        )`,
		`CREATE INDEX run_checkpoints_run_sequence_idx ON run_checkpoints(run_id, sequence)`,
		`CREATE TABLE checkpoint_instructions (
            checkpoint_id TEXT NOT NULL REFERENCES run_checkpoints(id) ON DELETE CASCADE,
            precedence INTEGER NOT NULL CHECK (precedence >= 0),
            path TEXT NOT NULL,
            scope_path TEXT NOT NULL,
            content_hash TEXT NOT NULL CHECK (length(content_hash) = 64),
            PRIMARY KEY (checkpoint_id, precedence),
            CHECK (length(trim(path)) > 0),
            CHECK (length(trim(scope_path)) > 0)
        )`,
	},
}}

func Migrate(ctx context.Context, database *Database) error {
	if database == nil || database.db == nil {
		return errors.New("SQLite migration database is nil")
	}
	return migrate(ctx, database.db, schemaMigrations, time.Now)
}

func CurrentSchemaVersion(ctx context.Context, database *Database) (int, error) {
	if database == nil || database.db == nil {
		return 0, errors.New("SQLite migration database is nil")
	}
	var version int
	err := database.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("query current SQLite schema version: %w", err)
	}
	return version, nil
}

func migrate(ctx context.Context, database *sql.DB, migrations []migration, now func() time.Time) error {
	if ctx == nil {
		return errors.New("SQLite migration context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if database == nil {
		return errors.New("SQLite migration SQL database is nil")
	}
	if now == nil {
		return errors.New("SQLite migration clock is nil")
	}
	if err := validateMigrations(migrations); err != nil {
		return err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SQLite migration transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        applied_at TEXT NOT NULL,
        CHECK (version >= 1),
        CHECK (length(trim(name)) > 0)
    )`); err != nil {
		return fmt.Errorf("create SQLite migration table: %w", err)
	}

	applied, err := loadAppliedMigrations(ctx, tx)
	if err != nil {
		return err
	}
	if err := validateAppliedMigrations(applied, migrations); err != nil {
		return err
	}
	for _, candidate := range migrations {
		if _, ok := applied[candidate.version]; ok {
			continue
		}
		for statementIndex, statement := range candidate.statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply SQLite migration %d %q statement %d: %w", candidate.version, candidate.name, statementIndex+1, err)
			}
		}
		appliedAt := now().UTC()
		if appliedAt.IsZero() {
			return errors.New("SQLite migration clock returned zero time")
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)`,
			candidate.version, candidate.name, appliedAt.Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("record SQLite migration %d %q: %w", candidate.version, candidate.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SQLite migrations: %w", err)
	}
	return nil
}

func validateMigrations(migrations []migration) error {
	for index, candidate := range migrations {
		expectedVersion := index + 1
		if candidate.version != expectedVersion {
			return fmt.Errorf("SQLite migration version %d is out of sequence; expected %d", candidate.version, expectedVersion)
		}
		if strings.TrimSpace(candidate.name) == "" {
			return fmt.Errorf("SQLite migration %d name is empty", candidate.version)
		}
		if len(candidate.statements) == 0 {
			return fmt.Errorf("SQLite migration %d has no statements", candidate.version)
		}
		for statementIndex, statement := range candidate.statements {
			if strings.TrimSpace(statement) == "" {
				return fmt.Errorf("SQLite migration %d statement %d is empty", candidate.version, statementIndex+1)
			}
		}
	}
	return nil
}

func loadAppliedMigrations(ctx context.Context, tx *sql.Tx) (map[int]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT version, name FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("query applied SQLite migrations: %w", err)
	}
	defer rows.Close()
	applied := make(map[int]string)
	for rows.Next() {
		var version int
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			return nil, fmt.Errorf("scan applied SQLite migration: %w", err)
		}
		applied[version] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied SQLite migrations: %w", err)
	}
	return applied, nil
}

func validateAppliedMigrations(applied map[int]string, migrations []migration) error {
	for version, name := range applied {
		if version < 1 || version > len(migrations) {
			return fmt.Errorf("%w: version %d", ErrUnknownSchemaVersion, version)
		}
		if migrations[version-1].name != name {
			return fmt.Errorf("%w: version %d name %q, expected %q", ErrMigrationDrift, version, name, migrations[version-1].name)
		}
	}
	for version := 1; version <= len(applied); version++ {
		if _, ok := applied[version]; !ok {
			return fmt.Errorf("%w: missing version %d", ErrMigrationDrift, version)
		}
	}
	return nil
}
