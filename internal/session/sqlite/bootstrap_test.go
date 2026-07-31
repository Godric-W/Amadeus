package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDatabasePathRequiresExplicitAbsoluteAmadeusRoot(t *testing.T) {
	for _, root := range []string{"", ".", "relative/root"} {
		if path, err := DatabasePath(root); err == nil || path != "" {
			t.Fatalf("unexpected database path for %q: path=%q err=%v", root, path, err)
		}
	}
	root := filepath.Clean(t.TempDir())
	path, err := DatabasePath(root)
	if err != nil {
		t.Fatalf("resolve database path: %v", err)
	}
	want := filepath.Join(root, "data", "amadeus.db")
	if path != want {
		t.Fatalf("database path = %q, want %q", path, want)
	}
}

func TestOpenCreatesSecureDatabaseAndConfiguresPragmas(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	database, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	path := database.Path()
	if path != filepath.Join(root, "data", "amadeus.db") || database.SQLDB() == nil {
		t.Fatalf("unexpected opened database: path=%q db=%#v", path, database.SQLDB())
	}
	assertMode(t, filepath.Join(root, "data"), 0o700)
	assertMode(t, path, 0o600)
	if err := verifyPragmas(context.Background(), database.SQLDB()); err != nil {
		t.Fatalf("verify SQLite pragmas: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close SQLite database: %v", err)
	}

	reopened, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("reopen SQLite database: %v", err)
	}
	defer reopened.Close()
	assertMode(t, filepath.Join(root, "data"), 0o700)
	assertMode(t, path, 0o600)
}

func TestOpenTightensExistingPermissions(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	dataPath := filepath.Join(root, "data")
	if err := os.Mkdir(dataPath, 0o755); err != nil {
		t.Fatalf("create data directory: %v", err)
	}
	databasePath := filepath.Join(dataPath, "amadeus.db")
	if err := os.WriteFile(databasePath, nil, 0o644); err != nil {
		t.Fatalf("create database file: %v", err)
	}
	database, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("open existing SQLite database: %v", err)
	}
	defer database.Close()
	assertMode(t, dataPath, 0o700)
	assertMode(t, databasePath, 0o600)
}

func TestOpenRejectsDataAndDatabaseSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	t.Run("data directory", func(t *testing.T) {
		root := filepath.Clean(t.TempDir())
		external := t.TempDir()
		if err := os.Symlink(external, filepath.Join(root, "data")); err != nil {
			t.Fatalf("create data symlink: %v", err)
		}
		if database, err := Open(context.Background(), root); err == nil || database != nil || !strings.Contains(err.Error(), "data directory cannot be a symlink") {
			t.Fatalf("unexpected data symlink result: database=%#v err=%v", database, err)
		}
	})
	t.Run("database", func(t *testing.T) {
		root := filepath.Clean(t.TempDir())
		dataPath := filepath.Join(root, "data")
		if err := os.Mkdir(dataPath, 0o700); err != nil {
			t.Fatalf("create data directory: %v", err)
		}
		external := filepath.Join(t.TempDir(), "outside.db")
		if err := os.WriteFile(external, nil, 0o600); err != nil {
			t.Fatalf("create external database: %v", err)
		}
		if err := os.Symlink(external, filepath.Join(dataPath, "amadeus.db")); err != nil {
			t.Fatalf("create database symlink: %v", err)
		}
		if database, err := Open(context.Background(), root); err == nil || database != nil || !strings.Contains(err.Error(), "database cannot be a symlink") {
			t.Fatalf("unexpected database symlink result: database=%#v err=%v", database, err)
		}
	})
}

func TestOpenHonorsCancellationAndRejectsMissingRoot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if database, err := Open(ctx, filepath.Clean(t.TempDir())); !errors.Is(err, context.Canceled) || database != nil {
		t.Fatalf("unexpected cancelled bootstrap: database=%#v err=%v", database, err)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if database, err := Open(context.Background(), missing); err == nil || database != nil {
		t.Fatalf("missing Amadeus root was created implicitly: database=%#v err=%v", database, err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
	}
}
