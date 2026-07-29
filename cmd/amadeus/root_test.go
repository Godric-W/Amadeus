package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
)

func TestVersionCommand(t *testing.T) {
	var output bytes.Buffer
	command := newRootCommand()
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"version"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute version command: %v", err)
	}

	const expected = "amadeus dev\ncommit: unknown\nbuild time: unknown\n"
	if output.String() != expected {
		t.Fatalf("unexpected version output: got %q, want %q", output.String(), expected)
	}
}

func TestResolveAmadeusRootPrefersEnvironment(t *testing.T) {
	lookupEnv := config.EnvLookup(func(name string) (string, bool) {
		if name == envAmadeusHome {
			return "/opt/amadeus", true
		}
		return "", false
	})

	root, err := resolveAmadeusRoot(
		lookupEnv,
		func() (string, error) { return "", errors.New("must not be called") },
		func(path string) (string, error) { return path, nil },
	)
	if err != nil {
		t.Fatalf("resolve environment Amadeus root: %v", err)
	}
	if root != "/opt/amadeus" {
		t.Fatalf("unexpected environment Amadeus root: got %q", root)
	}
}

func TestResolveAmadeusRootFallsBackToExecutableDirectory(t *testing.T) {
	root, err := resolveAmadeusRoot(
		emptyEnvLookup,
		func() (string, error) { return "/opt/amadeus/bin/amadeus", nil },
		func(path string) (string, error) { return path, nil },
	)
	if err != nil {
		t.Fatalf("resolve executable Amadeus root: %v", err)
	}
	if root != "/opt/amadeus/bin" {
		t.Fatalf("unexpected executable Amadeus root: got %q", root)
	}
}

func TestResolveAmadeusRootUsesResolvedExecutablePath(t *testing.T) {
	root, err := resolveAmadeusRoot(
		emptyEnvLookup,
		func() (string, error) { return "/usr/local/bin/amadeus", nil },
		func(string) (string, error) { return "/opt/amadeus/amadeus", nil },
	)
	if err != nil {
		t.Fatalf("resolve symlinked executable Amadeus root: %v", err)
	}
	if root != "/opt/amadeus" {
		t.Fatalf("unexpected resolved Amadeus root: got %q", root)
	}
}

func TestResolveAmadeusRootReturnsExecutableError(t *testing.T) {
	expected := errors.New("executable unavailable")
	_, err := resolveAmadeusRoot(
		emptyEnvLookup,
		func() (string, error) { return "", expected },
		func(path string) (string, error) { return path, nil },
	)
	if !errors.Is(err, expected) {
		t.Fatalf("unexpected executable error: got %v, want %v", err, expected)
	}
}
