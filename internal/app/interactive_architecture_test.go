package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFullscreenInteractiveArchitectureHasNoLegacyControlChain(t *testing.T) {
	root := repositoryRoot(t)
	paths := []string{
		filepath.Join(root, "cmd", "amadeus", "agent_interactive.go"),
		filepath.Join(root, "internal", "interface", "tui", "application.go"),
		filepath.Join(root, "internal", "interface", "tui", "application_update.go"),
		filepath.Join(root, "internal", "interface", "tui", "application_commands.go"),
	}
	forbidden := []string{
		"fullscreenTaskDoneMsg", "fullscreenCommandDoneMsg", "fullscreenResumeMsg", "fullscreenCompactMsg",
		"FullscreenSessionResumer", "FullscreenCompactor", "FullscreenMCPReader", "FullscreenPermissionModeSetter",
		"compactInteractiveSession", "writeInteractiveMCP", "replayTurnItems", ".runOnce(", ".waitTurn(",
	}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, symbol := range forbidden {
			if strings.Contains(string(content), symbol) {
				t.Fatalf("legacy symbol %q remains in %s", symbol, path)
			}
		}
	}
}

func TestInteractiveApplicationIsOnlyFullscreenSessionIOConsumer(t *testing.T) {
	root := repositoryRoot(t)
	applicationSource, err := os.ReadFile(filepath.Join(root, "internal", "app", "interactive_application.go"))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(applicationSource), "active.Io()"); count != 1 {
		t.Fatalf("interactive application SessionIo consumers = %d, want 1", count)
	}
	entrySource, err := os.ReadFile(filepath.Join(root, "cmd", "amadeus", "agent_interactive.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(entrySource), ".Io()") {
		t.Fatal("fullscreen CLI entry consumes SessionIo directly")
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
