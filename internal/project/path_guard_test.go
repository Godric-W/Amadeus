package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathGuardResolvesExistingPathsInsideRoot(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootPath, "real"), 0o700); err != nil {
		t.Fatalf("create real directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "real", "file.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("write real file: %v", err)
	}
	if err := os.Symlink(filepath.Join(rootPath, "real"), filepath.Join(rootPath, "alias")); err != nil {
		t.Fatalf("create internal symlink: %v", err)
	}
	guard := newTestPathGuard(t, rootPath)
	file, err := guard.ResolveExisting("alias/file.txt", PathFile)
	if err != nil || file != filepath.Join(rootPath, "real", "file.txt") {
		t.Fatalf("unexpected guarded file: path=%q err=%v", file, err)
	}
	directory, err := guard.ResolveExisting("alias", PathDirectory)
	if err != nil || directory != filepath.Join(rootPath, "real") {
		t.Fatalf("unexpected guarded directory: path=%q err=%v", directory, err)
	}
}

func TestPathGuardRejectsLexicalAndSymlinkEscapes(t *testing.T) {
	rootPath := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("write external file: %v", err)
	}
	if err := os.Symlink(external, filepath.Join(rootPath, "escape")); err != nil {
		t.Fatalf("create escaping symlink: %v", err)
	}
	guard := newTestPathGuard(t, rootPath)
	for _, path := range []string{"../outside", filepath.Join(string(filepath.Separator), "tmp", "outside"), "escape/secret.txt"} {
		if _, err := guard.ResolveExisting(path, PathAny); err == nil {
			t.Fatalf("expected guarded existing path %q to fail", path)
		}
	}
	if _, err := guard.ResolveForWrite("escape/new.txt"); err == nil || !strings.Contains(err.Error(), "outside project root") {
		t.Fatalf("unexpected guarded write escape error: %v", err)
	}
}

func TestPathGuardValidatesWriteAncestorsAndTargets(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootPath, "real"), 0o700); err != nil {
		t.Fatalf("create real directory: %v", err)
	}
	if err := os.Symlink(filepath.Join(rootPath, "real"), filepath.Join(rootPath, "alias")); err != nil {
		t.Fatalf("create internal directory symlink: %v", err)
	}
	guard := newTestPathGuard(t, rootPath)
	path, err := guard.ResolveForWrite("alias/new/deep.txt")
	if err != nil || path != filepath.Join(rootPath, "alias", "new", "deep.txt") {
		t.Fatalf("unexpected guarded write path: path=%q err=%v", path, err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "target.txt"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write target file: %v", err)
	}
	if err := os.Symlink(filepath.Join(rootPath, "target.txt"), filepath.Join(rootPath, "link.txt")); err != nil {
		t.Fatalf("create target symlink: %v", err)
	}
	if _, err := guard.ResolveForWrite("link.txt"); err == nil || !strings.Contains(err.Error(), "cannot be a symlink") {
		t.Fatalf("unexpected write symlink error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "parent"), []byte("file"), 0o600); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	if _, err := guard.ResolveForWrite("parent/child.txt"); err == nil || !strings.Contains(err.Error(), "parent is not a directory") {
		t.Fatalf("unexpected non-directory parent error: %v", err)
	}
}

func TestPathGuardValidatesTypesAndConstruction(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "file"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	guard := newTestPathGuard(t, rootPath)
	if _, err := guard.ResolveExisting("file", PathDirectory); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("unexpected directory type error: %v", err)
	}
	if _, err := guard.ResolveExisting(".", PathFile); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("unexpected file type error: %v", err)
	}
	if _, err := guard.ResolveExisting("file", "socket"); err == nil || !strings.Contains(err.Error(), "expected type") {
		t.Fatalf("unexpected invalid type error: %v", err)
	}
	if _, err := NewPathGuard(Root{}); err == nil {
		t.Fatal("expected empty root guard construction to fail")
	}
	var nilGuard *PathGuard
	if _, err := nilGuard.ResolveExisting(".", PathAny); err == nil {
		t.Fatal("expected nil path guard to fail")
	}
}

func newTestPathGuard(t *testing.T, rootPath string) *PathGuard {
	t.Helper()
	root, err := NewRoot(rootPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	guard, err := NewPathGuard(root)
	if err != nil {
		t.Fatalf("create path guard: %v", err)
	}
	return guard
}
