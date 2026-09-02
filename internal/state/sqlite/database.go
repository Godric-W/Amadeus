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

const (
	stateFilename = "state_1.sqlite"
	goalsFilename = "goals_1.sqlite"
)

type database struct {
	path string
	db   *sql.DB
}

func databasePath(amadeusHome, filename string) (string, error) {
	if strings.TrimSpace(amadeusHome) == "" || !filepath.IsAbs(amadeusHome) || filepath.Clean(amadeusHome) != amadeusHome {
		return "", errors.New("Amadeus home must be a clean absolute path")
	}
	if filepath.Base(filename) != filename || strings.TrimSpace(filename) == "" {
		return "", errors.New("state database filename is invalid")
	}
	return filepath.Join(amadeusHome, "data", filename), nil
}

func openDatabase(ctx context.Context, amadeusHome, filename string, schema databaseSchema) (*database, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := databasePath(amadeusHome, filename)
	if err != nil {
		return nil, err
	}
	_, statErr := os.Stat(path)
	isNew := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !isNew {
		return nil, fmt.Errorf("inspect %s database: %w", schema.label, statErr)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("secure state directory: %w", err)
	}
	db, err := sql.Open("sqlite", databaseDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open %s database: %w", schema.label, err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	opened := &database{path: path, db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping %s database: %w", schema.label, err)
	}
	if err := schema.ensure(ctx, db, path, isNew); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("secure %s database: %w", schema.label, err)
	}
	return opened, nil
}

func (database *database) close() error {
	if database == nil || database.db == nil {
		return nil
	}
	return database.db.Close()
}

func databaseDSN(path string) string {
	location := &url.URL{Scheme: "file", Path: sqliteURIPath(filepath.ToSlash(path))}
	query := location.Query()
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "synchronous(NORMAL)")
	query.Add("_pragma", "foreign_keys(ON)")
	location.RawQuery = query.Encode()
	return location.String()
}

func sqliteURIPath(path string) string {
	if len(path) >= 3 && isASCIILetter(path[0]) && path[1] == ':' && path[2] == '/' {
		return "/" + path
	}
	return path
}

func isASCIILetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}
