package bootstrap

import (
	"errors"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
)

func TestResolveAmadeusRootPrefersEnvironment(t *testing.T) {
	lookupEnv := config.EnvLookup(func(name string) (string, bool) {
		if name == EnvAmadeusHome {
			return "/opt/amadeus", true
		}
		return "", false
	})
	root, err := ResolveAmadeusRoot(lookupEnv, func() (string, error) {
		return "", errors.New("must not be called")
	}, func(path string) (string, error) { return path, nil })
	if err != nil || root != "/opt/amadeus" {
		t.Fatalf("resolve environment root: root=%q err=%v", root, err)
	}
}

func TestResolveAmadeusRootFallsBackToExecutableDirectory(t *testing.T) {
	root, err := ResolveAmadeusRoot(emptyEnvLookup, func() (string, error) {
		return "/opt/amadeus/bin/amadeus", nil
	}, func(path string) (string, error) { return path, nil })
	if err != nil || root != "/opt/amadeus/bin" {
		t.Fatalf("resolve executable root: root=%q err=%v", root, err)
	}
}

func TestResolveAmadeusRootUsesResolvedExecutablePath(t *testing.T) {
	root, err := ResolveAmadeusRoot(emptyEnvLookup, func() (string, error) {
		return "/usr/local/bin/amadeus", nil
	}, func(string) (string, error) { return "/opt/amadeus/amadeus", nil })
	if err != nil || root != "/opt/amadeus" {
		t.Fatalf("resolve symlinked root: root=%q err=%v", root, err)
	}
}

func TestResolveAmadeusRootReturnsExecutableError(t *testing.T) {
	expected := errors.New("executable unavailable")
	_, err := ResolveAmadeusRoot(emptyEnvLookup, func() (string, error) {
		return "", expected
	}, func(path string) (string, error) { return path, nil })
	if !errors.Is(err, expected) {
		t.Fatalf("unexpected executable error: got %v, want %v", err, expected)
	}
}

func emptyEnvLookup(string) (string, bool) { return "", false }
