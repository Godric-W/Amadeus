package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTargetPackageDependencyDirection(t *testing.T) {
	root := repositoryRoot(t)
	tests := []struct {
		root      string
		forbidden []string
	}{
		{root: "internal/protocol", forbidden: []string{"internal/agent/", "internal/app", "internal/bootstrap", "internal/cli", "internal/tui"}},
		{root: "internal/threadstore", forbidden: []string{"internal/agent/session", "internal/threadmanager", "internal/app", "internal/tui"}},
		{root: "internal/tool", forbidden: []string{"internal/tool/builtin", "internal/agent/session", "internal/app", "internal/tui"}},
		{root: "internal/agent", forbidden: []string{"internal/app", "internal/bootstrap", "internal/cli", "internal/tui"}},
		{root: "internal/contextmanager", forbidden: []string{"internal/app", "internal/bootstrap", "internal/cli", "internal/tui"}},
		{root: "internal/policy", forbidden: []string{"internal/app", "internal/bootstrap", "internal/cli", "internal/tui"}},
	}
	for _, test := range tests {
		scanProductionImports(t, root, test.root, func(path, imported string) {
			for _, forbidden := range test.forbidden {
				if strings.Contains(imported, forbidden) {
					t.Errorf("%s imports forbidden upper/concrete boundary %q", path, imported)
				}
			}
		})
	}
}

func TestThreadManagerIsOnlySessionSpawnOwner(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, relative), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(content), "agentsession.Spawn(") && !strings.HasSuffix(filepath.ToSlash(path), "internal/threadmanager/manager.go") {
				t.Errorf("Session spawn ownership leaked into %s", filepath.ToSlash(path[len(root)+1:]))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestTUIProjectionDoesNotLiveInApplicationPackage(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(root, "internal", "app", "transcript")); err == nil {
		t.Fatal("TUI transcript projection remains under internal/app")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, relative := range []string{"internal/app"} {
		err := filepath.WalkDir(filepath.Join(root, relative), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, forbidden := range []string{"type HistoryCell", "type TranscriptState", "type protocolEventState"} {
				if strings.Contains(string(content), forbidden) {
					t.Errorf("Application owns TUI projection %q in %s", forbidden, filepath.ToSlash(path[len(root)+1:]))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func scanProductionImports(t *testing.T, repositoryRoot, relativeRoot string, observe func(path, imported string)) {
	t.Helper()
	root := filepath.Join(repositoryRoot, filepath.FromSlash(relativeRoot))
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			observe(filepath.ToSlash(path[len(repositoryRoot)+1:]), imported)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s imports: %v", relativeRoot, err)
	}
}
