package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/rollout"
)

func TestOpenMigratesLegacyCanonicalHistory(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "data", "amadeus.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC)
	statements := []string{
		`CREATE TABLE projects (id TEXT PRIMARY KEY, canonical_path TEXT NOT NULL)`,
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, title TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE runs (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, sequence INTEGER NOT NULL, status TEXT NOT NULL, stop_reason TEXT, provider TEXT, model TEXT, started_at TEXT NOT NULL, finished_at TEXT)`,
		`CREATE TABLE rollout_items (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, run_id TEXT, sequence INTEGER NOT NULL, kind TEXT NOT NULL, payload_json TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`INSERT INTO projects(id, canonical_path) VALUES ('project-1', '` + filepath.ToSlash(filepath.Join(home, "workspace")) + `')`,
		`INSERT INTO sessions(id, project_id, title, status, created_at, updated_at) VALUES ('thread-1', 'project-1', 'Legacy', 'archived', '` + now.Format(time.RFC3339Nano) + `', '` + now.Add(time.Second).Format(time.RFC3339Nano) + `')`,
		`INSERT INTO runs(id, session_id, sequence, status, stop_reason, provider, model, started_at, finished_at) VALUES ('turn-1', 'thread-1', 1, 'completed', '', 'openai', 'gpt-test', '` + now.Format(time.RFC3339Nano) + `', '` + now.Add(time.Second).Format(time.RFC3339Nano) + `')`,
		`INSERT INTO rollout_items(id, session_id, run_id, sequence, kind, payload_json, created_at) VALUES ('item-1', 'thread-1', 'turn-1', 1, 'user_message', '{"content":"inspect files"}', '` + now.Format(time.RFC3339Nano) + `')`,
		`INSERT INTO rollout_items(id, session_id, run_id, sequence, kind, payload_json, created_at) VALUES ('item-2', 'thread-1', 'turn-1', 2, 'assistant_message', '{"content":"done"}', '` + now.Add(time.Second).Format(time.RFC3339Nano) + `')`,
	}
	for _, statement := range statements {
		if _, err := legacy.ExecContext(ctx, statement); err != nil {
			legacy.Close()
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := Open(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store, err := NewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := store.GetThread(ctx, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "inspect files" || metadata.Model != "gpt-test" || !metadata.Archived {
		t.Fatalf("metadata = %#v", metadata)
	}
	lines, err := rollout.Read(metadata.RolloutPath, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) < 5 || lines[0].Item.Kind != rollout.KindSessionMeta || lines[len(lines)-1].Item.Kind != rollout.KindTurnCompleted {
		t.Fatalf("migrated lines = %#v", lines)
	}
	for _, name := range []string{"projects", "sessions", "runs", "rollout_items"} {
		exists, err := tableExists(ctx, database.db, name)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("legacy table %s still exists", name)
		}
	}
	rows, err := database.db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if len(tables) != 2 || tables[0] != "schema_migrations" || tables[1] != "threads" {
		t.Fatalf("tables = %#v", tables)
	}
}

func TestOpenResumesLegacyMigrationFromExistingRollout(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "data", "amadeus.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 11, 2, 3, 4, 0, time.UTC)
	workspace := filepath.ToSlash(filepath.Join(home, "workspace"))
	statements := []string{
		`CREATE TABLE projects (id TEXT PRIMARY KEY, canonical_path TEXT NOT NULL)`,
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, title TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE runs (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, sequence INTEGER NOT NULL, status TEXT NOT NULL, stop_reason TEXT, provider TEXT, model TEXT, started_at TEXT NOT NULL, finished_at TEXT)`,
		`CREATE TABLE rollout_items (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, run_id TEXT, sequence INTEGER NOT NULL, kind TEXT NOT NULL, payload_json TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`INSERT INTO projects(id, canonical_path) VALUES ('project-1', '` + workspace + `')`,
		`INSERT INTO sessions(id, project_id, title, created_at, updated_at) VALUES ('thread-resume', 'project-1', 'Legacy Resume', '` + now.Format(time.RFC3339Nano) + `', '` + now.Add(time.Second).Format(time.RFC3339Nano) + `')`,
	}
	for _, statement := range statements {
		if _, err := legacy.ExecContext(ctx, statement); err != nil {
			legacy.Close()
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	rolloutPath := legacyRolloutPath(home, "thread-resume", now)
	recorder, err := rollout.Create(rolloutPath, "thread-resume", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	meta, err := rollout.NewItem(rollout.KindSessionMeta, rollout.SessionMeta{CWD: workspace, Title: "Legacy Resume", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Append(ctx, "", meta); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(ctx); err != nil {
		t.Fatal(err)
	}
	database, err := Open(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store, err := NewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := store.GetThread(ctx, "thread-resume")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.RolloutPath != rolloutPath || metadata.Title != "Legacy Resume" {
		t.Fatalf("metadata = %#v", metadata)
	}
	for _, name := range []string{"projects", "sessions", "runs", "rollout_items"} {
		exists, err := tableExists(ctx, database.db, name)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("legacy table %s still exists", name)
		}
	}
}
