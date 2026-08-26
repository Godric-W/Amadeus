package sqlite

import (
	"net/url"
	"reflect"
	"testing"
)

func TestDatabaseDSNUsesEmptyAuthorityForWindowsDrivePath(t *testing.T) {
	assertDatabaseDSN(t, "C:/Users/Test User/.amadeus/data/amadeus.db", "", "/C:/Users/Test User/.amadeus/data/amadeus.db")
}

func TestDatabaseDSNPreservesUnixAbsolutePath(t *testing.T) {
	assertDatabaseDSN(t, "/var/lib/amadeus/data/amadeus.db", "", "/var/lib/amadeus/data/amadeus.db")
}

func TestDatabaseDSNUsesPathInsteadOfAuthorityForUNCPath(t *testing.T) {
	assertDatabaseDSN(t, "//server/share/amadeus/data/amadeus.db", "", "//server/share/amadeus/data/amadeus.db")
}

func assertDatabaseDSN(t *testing.T, path, wantHost, wantPath string) {
	t.Helper()
	dsn := databaseDSN(path)
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN %q: %v", dsn, err)
	}
	if parsed.Scheme != "file" || parsed.Host != wantHost || parsed.Path != wantPath {
		t.Fatalf("DSN = %q, scheme=%q host=%q path=%q", dsn, parsed.Scheme, parsed.Host, parsed.Path)
	}
	wantPragmas := []string{"journal_mode(WAL)", "busy_timeout(5000)", "synchronous(NORMAL)"}
	if pragmas := parsed.Query()["_pragma"]; !reflect.DeepEqual(pragmas, wantPragmas) {
		t.Fatalf("DSN pragmas = %#v, want %#v", pragmas, wantPragmas)
	}
}
