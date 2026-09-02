package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const currentSchemaVersion = 1

var ErrUnsupportedSchema = errors.New("unsupported state schema")

type databaseSchema struct {
	label      string
	statements []string
	tables     []string
	indexes    []string
}

var threadSchema = databaseSchema{
	label: "thread state",
	statements: []string{
		`CREATE TABLE schema_info (version INTEGER PRIMARY KEY, created_at TEXT NOT NULL)`,
		`CREATE TABLE threads (
			id TEXT PRIMARY KEY,
			source_kind TEXT NOT NULL,
			parent_thread_id TEXT NOT NULL DEFAULT '',
			agent_depth INTEGER NOT NULL DEFAULT 0 CHECK (agent_depth >= 0),
			agent_nickname TEXT NOT NULL DEFAULT '',
			agent_role TEXT NOT NULL DEFAULT '',
			agent_edge_state TEXT NOT NULL DEFAULT '' CHECK (agent_edge_state IN ('', 'open', 'closed')),
			rollout_path TEXT NOT NULL UNIQUE,
			cwd TEXT NOT NULL,
			title TEXT NOT NULL,
			preview TEXT NOT NULL DEFAULT '',
			model_provider TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			tokens_used INTEGER NOT NULL DEFAULT 0 CHECK (tokens_used >= 0),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			archived INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
			git_sha TEXT NOT NULL DEFAULT '',
			git_branch TEXT NOT NULL DEFAULT '',
			git_origin_url TEXT NOT NULL DEFAULT '',
			CHECK (length(trim(id)) > 0),
			CHECK (length(trim(rollout_path)) > 0),
			CHECK (length(trim(cwd)) > 0),
			CHECK (length(trim(title)) > 0),
			CHECK (
				(source_kind = 'root' AND parent_thread_id = '' AND agent_depth = 0 AND agent_nickname = '' AND agent_role = '' AND agent_edge_state = '') OR
				(source_kind = 'subagent' AND length(trim(parent_thread_id)) > 0 AND agent_depth > 0 AND length(trim(agent_nickname)) > 0 AND length(trim(agent_role)) > 0)
			)
		)`,
		`CREATE INDEX threads_cwd_source_updated_idx ON threads(cwd, source_kind, archived, updated_at DESC, id)`,
	},
	tables:  []string{"schema_info", "threads"},
	indexes: []string{"threads_cwd_source_updated_idx"},
}

var goalSchema = databaseSchema{
	label: "goal state",
	statements: []string{
		`CREATE TABLE schema_info (version INTEGER PRIMARY KEY, created_at TEXT NOT NULL)`,
		`CREATE TABLE thread_goals (
			thread_id TEXT PRIMARY KEY NOT NULL,
			goal_id TEXT NOT NULL,
			objective TEXT NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('active', 'paused', 'blocked', 'usage_limited', 'budget_limited', 'complete')),
			token_budget INTEGER,
			tokens_used INTEGER NOT NULL DEFAULT 0,
			time_used_seconds INTEGER NOT NULL DEFAULT 0,
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL
		)`,
		`CREATE TABLE thread_goal_continuation_deferrals (
			thread_id TEXT PRIMARY KEY NOT NULL REFERENCES thread_goals(thread_id) ON DELETE CASCADE
		)`,
	},
	tables: []string{"schema_info", "thread_goal_continuation_deferrals", "thread_goals"},
}

func (schema databaseSchema) ensure(ctx context.Context, db *sql.DB, path string, isNew bool) error {
	if isNew {
		return schema.initialize(ctx, db)
	}
	if err := schema.validate(ctx, db); err != nil {
		return fmt.Errorf("%w: %s: %s", ErrUnsupportedSchema, path, strings.TrimSpace(err.Error()))
	}
	return nil
}

func (schema databaseSchema) initialize(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin %s schema initialization: %w", schema.label, err)
	}
	defer tx.Rollback()
	for _, statement := range schema.statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize %s schema: %w", schema.label, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_info(version, created_at) VALUES (?, ?)`, currentSchemaVersion, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("record %s schema version: %w", schema.label, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s schema initialization: %w", schema.label, err)
	}
	return nil
}

func (schema databaseSchema) validate(ctx context.Context, db *sql.DB) error {
	tables, err := schemaObjects(ctx, db, "table")
	if err != nil {
		return err
	}
	if !slices.Equal(tables, schema.tables) {
		return fmt.Errorf("database tables are %v, expected %v", tables, schema.tables)
	}
	var version int
	var createdAt string
	if err := db.QueryRowContext(ctx, `SELECT version, created_at FROM schema_info`).Scan(&version, &createdAt); err != nil {
		return fmt.Errorf("read schema_info: %w", err)
	}
	if version != currentSchemaVersion {
		return fmt.Errorf("schema_info.version is %d, expected %d", version, currentSchemaVersion)
	}
	if _, err := time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return fmt.Errorf("schema_info.created_at is invalid: %w", err)
	}
	indexes, err := schemaObjects(ctx, db, "index")
	if err != nil {
		return err
	}
	for _, expected := range schema.indexes {
		if !slices.Contains(indexes, expected) {
			return fmt.Errorf("database index %s is missing", expected)
		}
	}
	return nil
}

func schemaObjects(ctx context.Context, db *sql.DB, objectType string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type = ? AND name NOT LIKE 'sqlite_%' ORDER BY name`, objectType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
