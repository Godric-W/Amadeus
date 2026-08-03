package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRootResolutionDoesNotDependOnLaterWorkingDirectory(t *testing.T) {
	projectDir := t.TempDir()
	root, err := NewRoot(projectDir)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWorkingDirectory) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("change working directory: %v", err)
	}

	resolved, err := root.Resolve(filepath.Join("sub", "file.txt"))
	if err != nil {
		t.Fatalf("resolve project path: %v", err)
	}
	want := filepath.Join(projectDir, "sub", "file.txt")
	if resolved != want {
		t.Fatalf("working directory changed resolution: got %q, want %q", resolved, want)
	}
}

func TestRootRejectsAbsoluteAndLexicalEscape(t *testing.T) {
	root, err := NewRoot(t.TempDir())
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	for _, path := range []string{"../outside", filepath.Join("..", "outside", "file"), filepath.Join(string(filepath.Separator), "tmp", "outside")} {
		if _, err := root.Resolve(path); err == nil || !errors.Is(err, ErrPathOutsideRoot) {
			t.Fatalf("expected path rejection for %q", path)
		}
	}
}

func TestRootResolvesSymlinkAtConstruction(t *testing.T) {
	parent := t.TempDir()
	realRoot := filepath.Join(parent, "real")
	if err := os.Mkdir(realRoot, 0o755); err != nil {
		t.Fatalf("create real root: %v", err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatalf("create root symlink: %v", err)
	}
	root, err := NewRoot(link)
	if err != nil {
		t.Fatalf("create project root from symlink: %v", err)
	}
	if root.Path() != realRoot {
		t.Fatalf("root symlink was not stabilized: got %q, want %q", root.Path(), realRoot)
	}
}

func TestRootRelativeRejectsOutsidePaths(t *testing.T) {
	projectDir := t.TempDir()
	root, err := NewRoot(projectDir)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	inside := filepath.Join(projectDir, "a", "b.go")
	relative, err := root.Relative(inside)
	if err != nil || relative != "a/b.go" {
		t.Fatalf("unexpected relative path: %q, %v", relative, err)
	}
	if _, err := root.Relative(filepath.Dir(projectDir)); err == nil || !errors.Is(err, ErrPathOutsideRoot) {
		t.Fatal("expected outside path rejection")
	}
}
