package agentsmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestAgentsMdManagerLoadsUserAndScopedProjectDocuments(t *testing.T) {
	home := t.TempDir()
	rootPath := t.TempDir()
	nested := filepath.Join(rootPath, "pkg", "service")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAgentsMd(t, filepath.Join(home, FileName), "user rules")
	writeAgentsMd(t, filepath.Join(rootPath, FileName), "root rules")
	writeAgentsMd(t, filepath.Join(rootPath, "pkg", FileName), "package rules")
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home, []project.Root{root}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	loaded, changed, err := manager.Refresh(context.Background(), nested)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(loaded.Documents) != 3 {
		t.Fatalf("loaded AGENTS.md = %#v changed=%v", loaded, changed)
	}
	rendered := loaded.Render()
	if !strings.HasPrefix(rendered, "# AGENTS.md instructions for ") || !strings.Contains(rendered, "<INSTRUCTIONS>") || !strings.HasSuffix(rendered, "</INSTRUCTIONS>") || strings.Contains(rendered, "amadeus.agents_md") {
		t.Fatalf("AGENTS.md model fragment has the wrong contract: %s", rendered)
	}
	userIndex := strings.Index(rendered, "user rules")
	rootIndex := strings.Index(rendered, "root rules")
	packageIndex := strings.Index(rendered, "package rules")
	if userIndex < 0 || rootIndex < userIndex || packageIndex < rootIndex {
		t.Fatalf("AGENTS.md precedence order is wrong: %s", rendered)
	}
}

func TestAgentsMdManagerReportsTypedStaleForMutationTarget(t *testing.T) {
	home := t.TempDir()
	rootPath := t.TempDir()
	nested := filepath.Join(rootPath, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAgentsMd(t, filepath.Join(rootPath, FileName), "root rules")
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home, []project.Root{root}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	initial, _, err := manager.Refresh(context.Background(), rootPath)
	if err != nil {
		t.Fatal(err)
	}
	writeAgentsMd(t, filepath.Join(nested, FileName), "nested rules")
	target := tool.ContextTarget{Path: filepath.Join(nested, "file.go"), Kind: tool.ContextTargetFile, SideEffect: tool.SideEffectWrite}
	err = manager.ObserveTarget(context.Background(), target, tool.RequestSnapshot{AgentsMdRevision: initial.Revision})
	var stale *StaleError
	if !errors.As(err, &stale) || stale.ToolErrorKind() != "stale_agents_md" {
		t.Fatalf("stale result = %T %v", err, err)
	}
	current := manager.Current()
	if current.Revision == initial.Revision || !strings.Contains(current.Render(), "nested rules") {
		t.Fatalf("target observation did not refresh AGENTS.md: %#v", current)
	}
	if err := manager.ObserveTarget(context.Background(), target, tool.RequestSnapshot{AgentsMdRevision: current.Revision}); err != nil {
		t.Fatalf("current AGENTS.md snapshot remained stale: %v", err)
	}
}

func TestAgentsMdManagerReadObservationDefersContextRefreshToSession(t *testing.T) {
	home := t.TempDir()
	rootPath := t.TempDir()
	nested := filepath.Join(rootPath, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home, []project.Root{root}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	initial, _, err := manager.Refresh(context.Background(), rootPath)
	if err != nil {
		t.Fatal(err)
	}
	writeAgentsMd(t, filepath.Join(nested, FileName), "nested rules")
	target := tool.ContextTarget{Path: nested, Kind: tool.ContextTargetDirectory, SideEffect: tool.SideEffectRead}
	if err := manager.ObserveTarget(context.Background(), target, tool.RequestSnapshot{AgentsMdRevision: initial.Revision}); err != nil {
		t.Fatal(err)
	}
	if manager.Current().Revision == initial.Revision {
		t.Fatal("read target did not update manager snapshot for the next model step")
	}
}

func TestAgentsMdManagerRefreshDetectsDocumentChanges(t *testing.T) {
	home := t.TempDir()
	rootPath := t.TempDir()
	path := filepath.Join(rootPath, FileName)
	writeAgentsMd(t, path, "first")
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home, []project.Root{root}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := manager.Refresh(context.Background(), rootPath)
	if err != nil {
		t.Fatal(err)
	}
	writeAgentsMd(t, path, "second")
	second, changed, err := manager.Refresh(context.Background(), rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || first.Revision == second.Revision || !strings.Contains(second.Render(), "second") {
		t.Fatalf("changed AGENTS.md was not reloaded: first=%#v second=%#v", first, second)
	}
}

func writeAgentsMd(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
