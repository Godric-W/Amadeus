package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestListDirSortsFiltersHiddenAndLimits(t *testing.T) {
	rootPath := t.TempDir()
	for _, name := range []string{"b.txt", "a.txt", ".secret"} {
		if err := os.WriteFile(filepath.Join(rootPath, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(rootPath, "dir"), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	listDir := newTestListDir(t, rootPath, 2)

	result, err := listDir.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("list directory: %v", err)
	}
	if result.Text != "file\ta.txt\t5\nfile\tb.txt\t5" || !result.Partial || result.Metadata["hidden_omitted"] != 1 {
		t.Fatalf("unexpected directory listing: %#v", result)
	}
	visibleList := newTestListDir(t, rootPath, 10)
	visible, err := visibleList.Execute(context.Background(), json.RawMessage(`{"include_hidden":true,"limit":10}`))
	if err != nil || !strings.HasPrefix(visible.Text, "file\t.secret") || visible.Partial {
		t.Fatalf("unexpected hidden listing: result=%#v err=%v", visible, err)
	}
}

func TestGlobFilesSupportsRecursivePatternAndIgnoresBuildTrees(t *testing.T) {
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
	globFiles := newTestGlobFiles(t, rootPath, 10)

	result, err := globFiles.Execute(context.Background(), json.RawMessage(`{"pattern":"**/*.go"}`))
	if err != nil {
		t.Fatalf("glob files: %v", err)
	}
	if result.Text != "main.go\npkg/a.go\npkg/a_test.go" || result.Partial {
		t.Fatalf("unexpected glob result: %#v", result)
	}
}

func TestGlobFilesAppliesResultBudgetAndRejectsEscape(t *testing.T) {
	rootPath := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(rootPath, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	globFiles := newTestGlobFiles(t, rootPath, 2)
	result, err := globFiles.Execute(context.Background(), json.RawMessage(`{"pattern":"*.go"}`))
	if err != nil || result.Text != "a.go\nb.go" || !result.Partial {
		t.Fatalf("unexpected limited glob: result=%#v err=%v", result, err)
	}
	if _, err := globFiles.Execute(context.Background(), json.RawMessage(`{"pattern":"../*.go"}`)); err == nil {
		t.Fatal("expected escaping glob rejection")
	}
}

func newTestListDir(t *testing.T, rootPath string, maxEntries int) *ListDir {
	t.Helper()
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	listDir, err := NewListDir(root, ListDirOptions{MaxEntries: maxEntries})
	if err != nil {
		t.Fatalf("create list_dir: %v", err)
	}
	return listDir
}

func newTestGlobFiles(t *testing.T, rootPath string, maxResults int) *GlobFiles {
	t.Helper()
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	globFiles, err := NewGlobFiles(root, GlobFilesOptions{MaxResults: maxResults})
	if err != nil {
		t.Fatalf("create glob_files: %v", err)
	}
	return globFiles
}
