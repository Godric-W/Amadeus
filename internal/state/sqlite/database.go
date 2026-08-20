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
	_, statErr := os.Stat(path)
	isNew := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !isNew {
		return nil, fmt.Errorf("inspect state database: %w", statErr)
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
	if err := ensureCurrentSchema(ctx, database, path, isNew); err != nil {
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
