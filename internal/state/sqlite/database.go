package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type Database struct {
	path string
	db   *sql.DB
}

func DatabasePath(amadeusHome string) (string, error) {
	if strings.TrimSpace(amadeusHome) == "" || !filepath.IsAbs(amadeusHome) || filepath.Clean(amadeusHome) != amadeusHome {
		return "", errors.New("Amadeus home must be a clean absolute path")
	}
	return filepath.Join(amadeusHome, "data", "amadeus.db"), nil
}

func Open(ctx context.Context, amadeusHome string) (*Database, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := DatabasePath(amadeusHome)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("secure state directory: %w", err)
	}
	location := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := location.Query()
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "synchronous(NORMAL)")
	location.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", location.String())
	if err != nil {
		return nil, fmt.Errorf("open state database: %w", err)
	}
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(4)
	opened := &Database{path: path, db: database}
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("ping state database: %w", err)
	}
	if err := migrate(ctx, database); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("secure state database: %w", err)
	}
	return opened, nil
}

func (database *Database) Path() string {
	if database == nil {
		return ""
	}
	return database.path
}

func (database *Database) SQLDB() *sql.DB {
	if database == nil {
		return nil
	}
	return database.db
}

func (database *Database) Close() error {
	if database == nil || database.db == nil {
		return nil
	}
	return database.db.Close()
}

func migrate(ctx context.Context, database *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
            version INTEGER PRIMARY KEY,
            name TEXT NOT NULL UNIQUE,
            applied_at TEXT NOT NULL
        )`,
		`CREATE TABLE IF NOT EXISTS threads (
            id TEXT PRIMARY KEY,
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
            CHECK (length(trim(title)) > 0)
        )`,
		`CREATE INDEX IF NOT EXISTS threads_cwd_updated_idx ON threads(cwd, archived, updated_at DESC, id)`,
		`INSERT OR IGNORE INTO schema_migrations(version, name, applied_at)
            VALUES (100, 'thread_metadata_index', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`,
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin state migration: %w", err)
	}
	defer tx.Rollback()
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply state migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit state migration: %w", err)
	}
	return nil
}
