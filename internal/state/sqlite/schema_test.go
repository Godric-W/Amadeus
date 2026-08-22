package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenInitializesAndReopensCurrentSchema(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	database, err := Open(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := database.SQLDB().QueryRowContext(ctx, `SELECT version FROM schema_info`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != CurrentSchemaVersion {
		t.Fatalf("schema version = %d", version)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRejectsExistingEmptyDatabase(t *testing.T) {
	home := t.TempDir()
	path, err := DatabasePath(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Open(context.Background(), home)
	assertUnsupportedSchema(t, err)
}

func TestOpenRejectsWrongSchemaVersion(t *testing.T) {
	home := t.TempDir()
	database, err := Open(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQLDB().Exec(`UPDATE schema_info SET version = ?`, CurrentSchemaVersion+1); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Open(context.Background(), home)
	assertUnsupportedSchema(t, err)
}

func assertUnsupportedSchema(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("unsupported schema error = %v", err)
	}
}
