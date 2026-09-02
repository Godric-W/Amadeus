package sqlite

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestRuntimeOpensDedicatedCurrentSchemas(t *testing.T) {
	home := t.TempDir()
	runtime, err := Open(context.Background(), home, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{stateFilename, goalsFilename} {
		path := filepath.Join(home, "data", filename)
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("database %s: info=%v err=%v", filename, info, err)
		}
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), home, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRejectsExistingInvalidCurrentDatabase(t *testing.T) {
	home := t.TempDir()
	path, err := databasePath(home, stateFilename)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Open(context.Background(), home, time.Now)
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("error = %v", err)
	}
}

func TestDatabaseDSNUsesPathAndRequiredPragmas(t *testing.T) {
	for _, test := range []struct {
		path string
		want string
	}{
		{path: "C:/Users/Test User/.amadeus/data/state_1.sqlite", want: "/C:/Users/Test User/.amadeus/data/state_1.sqlite"},
		{path: "/var/lib/amadeus/data/state_1.sqlite", want: "/var/lib/amadeus/data/state_1.sqlite"},
		{path: "//server/share/amadeus/data/state_1.sqlite", want: "//server/share/amadeus/data/state_1.sqlite"},
	} {
		parsed, err := url.Parse(databaseDSN(test.path))
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Scheme != "file" || parsed.Host != "" || parsed.Path != test.want {
			t.Fatalf("DSN for %q = %#v", test.path, parsed)
		}
		want := []string{"journal_mode(WAL)", "busy_timeout(5000)", "synchronous(NORMAL)", "foreign_keys(ON)"}
		if got := parsed.Query()["_pragma"]; !reflect.DeepEqual(got, want) {
			t.Fatalf("pragmas = %#v", got)
		}
	}
}
