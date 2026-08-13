package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestGlobSupportsRecursivePatternAndIgnoresBuildTrees(t *testing.T) {
	rootPath := t.TempDir()
	files := []string{"main.go", "pkg/a.go", "pkg/a_test.go", ".hidden.go", ".hidden/x.go", ".git/config", "vendor/v.go", "build/generated.go"}
	for _, name := range files {
		path := filepath.Join(rootPath, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture parent: %v", err)
		}
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
	glob := newTestGlob(t, rootPath, 10)
	result, err := executePreparedTool(t, context.Background(), glob, json.RawMessage(`{"pattern":"**/*.go"}`))
	if err != nil {
		t.Fatalf("glob files: %v", err)
	}
	if result.Text != "main.go\npkg/a.go\npkg/a_test.go" || result.Partial {
		t.Fatalf("unexpected glob result: %#v", result)
	}
}

func TestGlobAppliesResultBudgetAndRejectsEscape(t *testing.T) {
	rootPath := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(rootPath, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	glob := newTestGlob(t, rootPath, 2)
	result, err := executePreparedTool(t, context.Background(), glob, json.RawMessage(`{"pattern":"*.go"}`))
	if err != nil || result.Text != "a.go\nb.go" || !result.Partial {
		t.Fatalf("unexpected limited glob: result=%#v err=%v", result, err)
	}
	if _, err := executePreparedTool(t, context.Background(), glob, json.RawMessage(`{"pattern":"../*.go"}`)); err == nil {
		t.Fatal("expected escaping glob rejection")
	}
}

func newTestGlob(t *testing.T, rootPath string, maxResults int) *Glob {
	t.Helper()
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	glob, err := NewGlob(root, GlobOptions{MaxResults: maxResults})
	if err != nil {
		t.Fatalf("create glob: %v", err)
	}
	return glob
}
