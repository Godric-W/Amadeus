package bootstrap

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
)

func TestResolveAuditPathUsesXDGThenHome(t *testing.T) {
	lookup := config.EnvLookup(func(name string) (string, bool) {
		if name == EnvXDGStateHome {
			return "/state", true
		}
		return "", false
	})
	path, err := ResolveAuditPath(lookup, func() (string, error) { return "", errors.New("must not be called") })
	if err != nil || path != filepath.Join("/state", "amadeus", "audit", "audit.jsonl") {
		t.Fatalf("unexpected XDG audit path: path=%q err=%v", path, err)
	}
	path, err = ResolveAuditPath(emptyEnvLookup, func() (string, error) { return "/home/test", nil })
	if err != nil || path != filepath.Join("/home/test", ".local", "state", "amadeus", "audit", "audit.jsonl") {
		t.Fatalf("unexpected home audit path: path=%q err=%v", path, err)
	}
	if _, err := ResolveAuditPath(emptyEnvLookup, nil); err == nil {
		t.Fatal("nil home resolver did not fail")
	}
}
