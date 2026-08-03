package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestOpenMigratesNewDatabaseToSixTableSchema(t *testing.T) {
	database, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open migrated SQLite database: %v", err)
	}
	defer database.Close()
	version, err := CurrentSchemaVersion(context.Background(), database)
	if err != nil || version != 4 {
		t.Fatalf("unexpected schema version: version=%d err=%v", version, err)
	}
	tables := sqliteTableNames(t, database)
	want := []string{
		"conversation_messages", "conversation_sessions", "conversation_summaries",
		"projects", "runs", "schema_migrations",
	}
	if !reflect.DeepEqual(tables, want) {
		t.Fatalf("unexpected SQLite tables: got %v, want %v", tables, want)
	}
	var name string
	if err := database.SQLDB().QueryRow(`SELECT name FROM schema_migrations WHERE version = 1`).Scan(&name); err != nil || name != "initial_session_schema" {
		t.Fatalf("unexpected migration history: name=%q err=%v", name, err)
	}
	if _, err := database.SQLDB().Exec(`INSERT INTO conversation_sessions(
		id, project_id, title, status, next_run_sequence, created_at, updated_at, last_active_at
    ) VALUES ('session', 'missing-project', 'Title', 'active', 1, 'now', 'now', 'now')`); err == nil {
		t.Fatal("foreign key enforcement allowed session without project")
	}
}

func TestOpenRepeatedlyDoesNotDuplicateMigrationHistory(t *testing.T) {
	root := t.TempDir()
	first, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}
	second, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer second.Close()
	var count int
	if err := second.SQLDB().QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("migration history duplicated: count=%d err=%v", count, err)
	}
}

func TestMigrationPreservesLegacyRunMessageAndCheckpointContext(t *testing.T) {
	root := t.TempDir()
	path, err := DatabasePath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	ctx := context.Background()
	now := func() time.Time { return time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC) }
	if err := migrate(ctx, legacy, schemaMigrations[:1], now); err != nil {
		t.Fatalf("create legacy v1 schema: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO projects(id, canonical_path, display_name, created_at, updated_at, last_opened_at) VALUES ('project-1', '/tmp/legacy', 'legacy', '2026-07-31T00:00:00Z', '2026-07-31T00:00:00Z', '2026-07-31T00:00:00Z')`,
		`INSERT INTO conversation_sessions(id, project_id, title, status, next_turn_sequence, created_at, updated_at, last_active_at) VALUES ('session-1', 'project-1', 'legacy', 'active', 2, '2026-07-31T00:00:00Z', '2026-07-31T00:00:00Z', '2026-07-31T00:00:00Z')`,
		`INSERT INTO session_turns(id, session_id, sequence, status, created_at, completed_at) VALUES ('turn-1', 'session-1', 1, 'cancelled', '2026-07-31T00:00:00Z', '2026-07-31T00:00:01Z')`,
		`INSERT INTO runs(id, session_id, turn_id, context_from_run_id, objective, status, stop_reason, provider, model, api_mode, dialect, budget_json, usage_json, latest_checkpoint_seq, started_at, finished_at) VALUES ('run-1', 'session-1', 'turn-1', NULL, 'legacy objective', 'cancelled', 'cancelled', 'openai', 'model', 'responses', 'openai', '{"max_steps":3}', '{"input_tokens":1}', 1, '2026-07-31T00:00:00Z', '2026-07-31T00:00:01Z')`,
		`INSERT INTO conversation_messages(id, session_id, turn_id, sequence, role, content, created_at) VALUES ('message-1', 'session-1', 'turn-1', 1, 'user', 'legacy request', '2026-07-31T00:00:00Z')`,
		`INSERT INTO run_checkpoints(id, run_id, sequence, schema_version, reason, payload_json, payload_hash, created_at) VALUES ('checkpoint-1', 'run-1', 1, 1, 'user_cancelled', '{"objective":"legacy objective","pending_work":["retry"]}', '0000000000000000000000000000000000000000000000000000000000000000', '2026-07-31T00:00:01Z')`,
	} {
		if _, err := legacy.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed legacy database: %v\nstatement: %s", err, statement)
		}
	}
	if err := migrate(ctx, legacy, schemaMigrations, now); err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	var status, executionMode, contextJSON, messageRunID string
	var sequence int
	if err := legacy.QueryRowContext(ctx, `SELECT status, sequence, execution_mode, interrupted_context_json FROM runs WHERE id = 'run-1'`).Scan(&status, &sequence, &executionMode, &contextJSON); err != nil {
		t.Fatal(err)
	}
	if status != "interrupted" || sequence != 1 || executionMode != "react" || contextJSON == "" || !strings.Contains(contextJSON, "legacy objective") {
		t.Fatalf("legacy run was not preserved: status=%q sequence=%d mode=%q context=%q", status, sequence, executionMode, contextJSON)
	}
	if err := legacy.QueryRowContext(ctx, `SELECT run_id FROM conversation_messages WHERE id = 'message-1'`).Scan(&messageRunID); err != nil || messageRunID != "run-1" {
		t.Fatalf("legacy message was not mapped to run: run_id=%q err=%v", messageRunID, err)
	}
	var oldTables int
	if err := legacy.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('session_turns', 'run_checkpoints', 'checkpoint_instructions')`).Scan(&oldTables); err != nil || oldTables != 0 {
		t.Fatalf("legacy tables remained after migration: count=%d err=%v", oldTables, err)
	}
}

func TestOpenRejectsUnknownAndDriftedMigrationHistory(t *testing.T) {
	t.Run("unknown version", func(t *testing.T) {
		root := t.TempDir()
		database, err := Open(context.Background(), root)
		if err != nil {
			t.Fatalf("open database: %v", err)
		}
		if _, err := database.SQLDB().Exec(`INSERT INTO schema_migrations(version, name, applied_at) VALUES (99, 'future', '2026-07-31T00:00:00Z')`); err != nil {
			t.Fatalf("insert future migration: %v", err)
		}
		if err := database.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
		reopened, err := Open(context.Background(), root)
		if !errors.Is(err, ErrUnknownSchemaVersion) || reopened != nil {
			t.Fatalf("unknown schema version was accepted: database=%#v err=%v", reopened, err)
		}
	})
	t.Run("name drift", func(t *testing.T) {
		root := t.TempDir()
		database, err := Open(context.Background(), root)
		if err != nil {
			t.Fatalf("open database: %v", err)
		}
		if _, err := database.SQLDB().Exec(`UPDATE schema_migrations SET name = 'changed' WHERE version = 1`); err != nil {
			t.Fatalf("drift migration name: %v", err)
		}
		if err := database.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
		reopened, err := Open(context.Background(), root)
		if !errors.Is(err, ErrMigrationDrift) || reopened != nil {
			t.Fatalf("migration drift was accepted: database=%#v err=%v", reopened, err)
		}
	})
}

func TestMigrationFailureRollsBackSchemaAndHistory(t *testing.T) {
	database, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	migrations := append([]migration(nil), schemaMigrations...)
	migrations = append(migrations, migration{
		version: 5, name: "rollback_probe",
		statements: []string{
			`CREATE TABLE rollback_probe (id INTEGER PRIMARY KEY)`,
			`THIS IS NOT VALID SQL`,
		},
	})
	err = migrate(context.Background(), database.SQLDB(), migrations, func() time.Time {
		return time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	})
	if err == nil {
		t.Fatal("broken migration unexpectedly succeeded")
	}
	var count int
	if err := database.SQLDB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'rollback_probe'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed migration left schema changes: count=%d err=%v", count, err)
	}
	if err := database.SQLDB().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 5`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed migration left history: count=%d err=%v", count, err)
	}
}

func TestMigrationDefinitionValidationRejectsGapsAndEmptyStatements(t *testing.T) {
	database, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	tests := [][]migration{
		{{version: 2, name: "gap", statements: []string{"SELECT 1"}}},
		{{version: 1, name: "", statements: []string{"SELECT 1"}}},
		{{version: 1, name: "empty", statements: []string{" "}}},
	}
	for _, migrations := range tests {
		if err := migrate(context.Background(), database.SQLDB(), migrations, time.Now); err == nil {
			t.Fatalf("invalid migration definition was accepted: %#v", migrations)
		}
	}
}

func sqliteTableNames(t *testing.T, database *Database) []string {
	t.Helper()
	rows, err := database.SQLDB().Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("query SQLite tables: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan SQLite table: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate SQLite tables: %v", err)
	}
	sort.Strings(names)
	return names
}
