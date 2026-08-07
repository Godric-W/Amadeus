package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestOpenCreatesCanonicalSchema(t *testing.T) {
	database, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	version, err := CurrentSchemaVersion(context.Background(), database)
	if err != nil || version != 5 {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	want := []string{"projects", "rollout_items", "runs", "schema_migrations", "sessions"}
	if got := sqliteTableNames(t, database.SQLDB()); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected tables: got %v want %v", got, want)
	}
}

func TestCanonicalMigrationDropsDevelopmentConversationTables(t *testing.T) {
	database, err := sql.Open("sqlite", "file:canonical-migration?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open memory database: %v", err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := migrate(ctx, database, schemaMigrations[:4], func() time.Time { return time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC) }); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	if err := migrate(ctx, database, schemaMigrations, func() time.Time { return time.Date(2026, 8, 5, 0, 0, 1, 0, time.UTC) }); err != nil {
		t.Fatalf("migrate canonical schema: %v", err)
	}
	want := []string{"projects", "rollout_items", "runs", "schema_migrations", "sessions"}
	if got := sqliteTableNames(t, database); !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy tables remain: got %v want %v", got, want)
	}
}

func TestOpenRejectsUnknownAndDriftedMigrationHistory(t *testing.T) {
	t.Run("unknown", func(t *testing.T) {
		root := t.TempDir()
		database, err := Open(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.SQLDB().Exec(`INSERT INTO schema_migrations(version, name, applied_at) VALUES (99, 'future', '2026-08-05T00:00:00Z')`); err != nil {
			t.Fatal(err)
		}
		database.Close()
		reopened, err := Open(context.Background(), root)
		if !errors.Is(err, ErrUnknownSchemaVersion) || reopened != nil {
			t.Fatalf("unknown schema accepted: db=%#v err=%v", reopened, err)
		}
	})
	t.Run("drift", func(t *testing.T) {
		root := t.TempDir()
		database, err := Open(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.SQLDB().Exec(`UPDATE schema_migrations SET name = 'changed' WHERE version = 5`); err != nil {
			t.Fatal(err)
		}
		database.Close()
		reopened, err := Open(context.Background(), root)
		if !errors.Is(err, ErrMigrationDrift) || reopened != nil {
			t.Fatalf("drift accepted: db=%#v err=%v", reopened, err)
		}
	})
}

func TestMigrationFailureRollsBack(t *testing.T) {
	database, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	migrations := append([]migration(nil), schemaMigrations...)
	migrations = append(migrations, migration{version: 6, name: "rollback_probe", statements: []string{`CREATE TABLE rollback_probe (id INTEGER PRIMARY KEY)`, `THIS IS NOT VALID SQL`}})
	if err := migrate(context.Background(), database.SQLDB(), migrations, time.Now); err == nil {
		t.Fatal("broken migration succeeded")
	}
	var count int
	if err := database.SQLDB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='rollback_probe'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback table count=%d err=%v", count, err)
	}
}

func sqliteTableNames(t *testing.T, database *sql.DB) []string {
	t.Helper()
	rows, err := database.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	return names
}
