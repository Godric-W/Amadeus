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
	dataDirectoryName = "data"
	databaseFileName  = "amadeus.db"
)

type Database struct {
	path string
	db   *sql.DB
}

func DatabasePath(amadeusRoot string) (string, error) {
	amadeusRoot = strings.TrimSpace(amadeusRoot)
	if amadeusRoot == "" {
		return "", errors.New("SQLite Amadeus root is empty")
	}
	if !filepath.IsAbs(amadeusRoot) || filepath.Clean(amadeusRoot) != amadeusRoot {
		return "", errors.New("SQLite Amadeus root must be a clean absolute path")
	}
	return filepath.Join(amadeusRoot, dataDirectoryName, databaseFileName), nil
}

func Open(ctx context.Context, amadeusRoot string) (*Database, error) {
	if ctx == nil {
		return nil, errors.New("SQLite bootstrap context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := DatabasePath(amadeusRoot)
	if err != nil {
		return nil, err
	}
	if err := preparePaths(amadeusRoot, path); err != nil {
		return nil, err
	}

	database, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open SQLite database %q: %w", path, err)
	}
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(4)
	closeOnError := func(openErr error) (*Database, error) {
		return nil, errors.Join(openErr, database.Close())
	}
	if err := database.PingContext(ctx); err != nil {
		return closeOnError(fmt.Errorf("ping SQLite database %q: %w", path, err))
	}
	if err := secureDatabaseFile(path); err != nil {
		return closeOnError(err)
	}
	if err := verifyPragmas(ctx, database); err != nil {
		return closeOnError(err)
	}
	opened := &Database{path: path, db: database}
	if err := Migrate(ctx, opened); err != nil {
		return closeOnError(err)
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

func preparePaths(amadeusRoot, databasePath string) error {
	rootInfo, err := os.Stat(amadeusRoot)
	if err != nil {
		return fmt.Errorf("inspect SQLite Amadeus root %q: %w", amadeusRoot, err)
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("SQLite Amadeus root is not a directory: %q", amadeusRoot)
	}
	dataPath := filepath.Dir(databasePath)
	info, err := os.Lstat(dataPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.Mkdir(dataPath, 0o700); err != nil {
			return fmt.Errorf("create SQLite data directory %q: %w", dataPath, err)
		}
	case err != nil:
		return fmt.Errorf("inspect SQLite data directory %q: %w", dataPath, err)
	case info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("SQLite data directory cannot be a symlink: %q", dataPath)
	case !info.IsDir():
		return fmt.Errorf("SQLite data path is not a directory: %q", dataPath)
	}
	if err := os.Chmod(dataPath, 0o700); err != nil {
		return fmt.Errorf("secure SQLite data directory %q: %w", dataPath, err)
	}
	if info, err := os.Lstat(databasePath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("SQLite database cannot be a symlink: %q", databasePath)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("SQLite database path is not a regular file: %q", databasePath)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect SQLite database %q: %w", databasePath, err)
	}
	return nil
}

func secureDatabaseFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect created SQLite database %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("created SQLite database is not a regular file: %q", path)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure SQLite database %q: %w", path, err)
	}
	return nil
}

func sqliteDSN(path string) string {
	location := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := location.Query()
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "synchronous(NORMAL)")
	location.RawQuery = query.Encode()
	return location.String()
}

func verifyPragmas(ctx context.Context, database *sql.DB) error {
	var foreignKeys int
	if err := database.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return fmt.Errorf("query SQLite foreign_keys pragma: %w", err)
	}
	if foreignKeys != 1 {
		return fmt.Errorf("SQLite foreign_keys pragma = %d, want 1", foreignKeys)
	}
	var journalMode string
	if err := database.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return fmt.Errorf("query SQLite journal_mode pragma: %w", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		return fmt.Errorf("SQLite journal_mode pragma = %q, want WAL", journalMode)
	}
	var busyTimeout int
	if err := database.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		return fmt.Errorf("query SQLite busy_timeout pragma: %w", err)
	}
	if busyTimeout != 5000 {
		return fmt.Errorf("SQLite busy_timeout pragma = %d, want 5000", busyTimeout)
	}
	var synchronous int
	if err := database.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
		return fmt.Errorf("query SQLite synchronous pragma: %w", err)
	}
	if synchronous != 1 {
		return fmt.Errorf("SQLite synchronous pragma = %d, want NORMAL(1)", synchronous)
	}
	return nil
}
