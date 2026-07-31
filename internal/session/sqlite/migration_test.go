package sqlite

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestOpenMigratesNewDatabaseToNineTableSchema(t *testing.T) {
	database, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open migrated SQLite database: %v", err)
	}
	defer database.Close()
	version, err := CurrentSchemaVersion(context.Background(), database)
	if err != nil || version != 1 {
		t.Fatalf("unexpected schema version: version=%d err=%v", version, err)
	}
	tables := sqliteTableNames(t, database)
	want := []string{
		"checkpoint_instructions", "conversation_messages", "conversation_sessions", "conversation_summaries",
		"projects", "run_checkpoints", "runs", "schema_migrations", "session_turns",
	}
	if !reflect.DeepEqual(tables, want) {
		t.Fatalf("unexpected SQLite tables: got %v, want %v", tables, want)
	}
	var name string
	if err := database.SQLDB().QueryRow(`SELECT name FROM schema_migrations WHERE version = 1`).Scan(&name); err != nil || name != "initial_session_schema" {
		t.Fatalf("unexpected migration history: name=%q err=%v", name, err)
	}
	if _, err := database.SQLDB().Exec(`INSERT INTO conversation_sessions(
        id, project_id, title, status, next_turn_sequence, created_at, updated_at, last_active_at
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
	if err := second.SQLDB().QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration history duplicated: count=%d err=%v", count, err)
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
		version: 2, name: "rollback_probe",
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
	if err := database.SQLDB().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 2`).Scan(&count); err != nil || count != 0 {
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
