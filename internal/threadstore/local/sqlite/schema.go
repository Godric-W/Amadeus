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

const CurrentSchemaVersion = 4

var ErrUnsupportedSchema = errors.New("unsupported state schema")

var currentSchemaStatements = []string{
	`CREATE TABLE schema_info (
        version INTEGER PRIMARY KEY,
        created_at TEXT NOT NULL
    )`,
	`CREATE TABLE threads (
        id TEXT PRIMARY KEY,
        source_kind TEXT NOT NULL,
        parent_thread_id TEXT NOT NULL DEFAULT '',
        agent_depth INTEGER NOT NULL DEFAULT 0 CHECK (agent_depth >= 0),
        agent_nickname TEXT NOT NULL DEFAULT '',
        agent_role TEXT NOT NULL DEFAULT '',
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
            (source_kind = 'root' AND parent_thread_id = '' AND agent_depth = 0 AND agent_nickname = '' AND agent_role = '') OR
            (source_kind = 'subagent' AND length(trim(parent_thread_id)) > 0 AND agent_depth > 0 AND length(trim(agent_nickname)) > 0 AND length(trim(agent_role)) > 0)
        )
    )`,
	`CREATE INDEX threads_cwd_source_updated_idx ON threads(cwd, source_kind, archived, updated_at DESC, id)`,
}

func ensureCurrentSchema(ctx context.Context, database *sql.DB, path string, isNew bool) error {
	if isNew {
		return initializeCurrentSchema(ctx, database)
	}
	if err := validateCurrentSchema(ctx, database); err != nil {
		return unsupportedSchema(path, err)
	}
	return nil
}

func initializeCurrentSchema(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin state schema initialization: %w", err)
	}
	defer tx.Rollback()
	for _, statement := range currentSchemaStatements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize state schema: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_info(version, created_at) VALUES (?, ?)`, CurrentSchemaVersion, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("record state schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit state schema initialization: %w", err)
	}
	return nil
}

func validateCurrentSchema(ctx context.Context, database *sql.DB) error {
	tables, err := schemaObjects(ctx, database, "table")
	if err != nil {
		return err
	}
	expectedTables := []string{"schema_info", "threads"}
	if !slices.Equal(tables, expectedTables) {
		return fmt.Errorf("database tables are %v, expected %v", tables, expectedTables)
	}
	rows, err := database.QueryContext(ctx, `SELECT version, created_at FROM schema_info ORDER BY version`)
	if err != nil {
		return fmt.Errorf("read schema_info: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var version int
		var createdAt string
		if err := rows.Scan(&version, &createdAt); err != nil {
			return fmt.Errorf("scan schema_info: %w", err)
		}
		if version != CurrentSchemaVersion {
			return fmt.Errorf("schema_info.version is %d, expected %d", version, CurrentSchemaVersion)
		}
		if _, err := time.Parse(time.RFC3339Nano, createdAt); err != nil {
			return fmt.Errorf("schema_info.created_at is invalid: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate schema_info: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("schema_info contains %d rows, expected 1", count)
	}
	indexes, err := schemaObjects(ctx, database, "index")
	if err != nil {
		return err
	}
	if !slices.Contains(indexes, "threads_cwd_source_updated_idx") {
		return errors.New("threads_cwd_source_updated_idx is missing")
	}
	return nil
}

func schemaObjects(ctx context.Context, database *sql.DB, objectType string) ([]string, error) {
	rows, err := database.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type = ? AND name NOT LIKE 'sqlite_%' ORDER BY name`, objectType)
	if err != nil {
		return nil, fmt.Errorf("inspect state schema %s objects: %w", objectType, err)
	}
	defer rows.Close()
	objects := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan state schema %s object: %w", objectType, err)
		}
		objects = append(objects, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate state schema %s objects: %w", objectType, err)
	}
	return objects, nil
}

func unsupportedSchema(path string, cause error) error {
	_ = path
	return fmt.Errorf("%w: %s", ErrUnsupportedSchema, strings.TrimSpace(cause.Error()))
}
