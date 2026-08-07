package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileSystemPolicyReadHostAndPermissionStores(t *testing.T) {
	workspace := t.TempDir()
	external := t.TempDir()
	runStore := NewPermissionStore()
	sessionStore := NewPermissionStore()
	policy, err := NewFileSystemPolicy(FileSystemPolicyOptions{
		CWD:            workspace,
		Profile:        PermissionProfile{ReadHost: true, WorkspaceRoots: []string{workspace}},
		RunPermissions: runStore, SessionPermissions: sessionStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(external, "read.txt")
	if err := os.WriteFile(file, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if resolved, err := policy.ResolveExisting(file, PathFile); err != nil || resolved.RootSource != RootSourceHost {
		t.Fatalf("host read: %#v %v", resolved, err)
	}
	target := filepath.Join(external, "write.txt")
	if _, err := policy.ResolveForWrite(target); !errors.Is(err, ErrPermissionRequired) {
		t.Fatalf("expected permission_required, got %v", err)
	}
	if err := runStore.GrantWritableRoots([]string{external}); err != nil {
		t.Fatal(err)
	}
	if resolved, err := policy.ResolveForWrite(target); err != nil || resolved.RootSource != RootSourceRun {
		t.Fatalf("run grant: %#v %v", resolved, err)
	}
	runStore.Clear()
	if err := sessionStore.GrantWritableRoots([]string{external}); err != nil {
		t.Fatal(err)
	}
	if resolved, err := policy.ResolveForWrite(target); err != nil || resolved.RootSource != RootSourceSession {
		t.Fatalf("session grant: %#v %v", resolved, err)
	}
}

func TestFileSystemPolicyReadOnlyAndDeniedCannotBeGranted(t *testing.T) {
	workspace := t.TempDir()
	readOnly := filepath.Join(workspace, "readonly")
	denied := filepath.Join(workspace, "denied")
	if err := os.MkdirAll(readOnly, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(denied, 0o700); err != nil {
		t.Fatal(err)
	}
	store := NewPermissionStore()
	if err := store.GrantWritableRoots([]string{readOnly, denied}); err != nil {
		t.Fatal(err)
	}
	policy, err := NewFileSystemPolicy(FileSystemPolicyOptions{
		CWD:            workspace,
		Profile:        PermissionProfile{ReadHost: true, WorkspaceRoots: []string{workspace}, ReadOnlyRoots: []string{readOnly}, DeniedRoots: []string{denied}},
		RunPermissions: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.ResolveForWrite(filepath.Join(readOnly, "x")); !errors.Is(err, ErrPathDenied) {
		t.Fatalf("read-only grant bypassed: %v", err)
	}
	if _, err := policy.ResolveExisting(denied, PathDirectory); !errors.Is(err, ErrPathDenied) {
		t.Fatalf("denied read bypassed: %v", err)
	}
	if _, err := policy.ResolveGrantRoot(readOnly); !errors.Is(err, ErrPathDenied) {
		t.Fatalf("read-only permission request accepted: %v", err)
	}
}

func TestPathGuardCanonicalizesSymlinksAndWriteAncestors(t *testing.T) {
	workspace := t.TempDir()
	real := filepath.Join(workspace, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(workspace, "alias")); err != nil {
		t.Fatal(err)
	}
	guard, err := NewPathGuard(NewRootForTest(t, workspace))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := guard.ResolveForWrite("alias/new/file.txt")
	if err != nil || resolved != filepath.Join(real, "new", "file.txt") {
		t.Fatalf("canonical write: %q %v", resolved, err)
	}
}

func NewRootForTest(t *testing.T, path string) Root {
	t.Helper()
	root, err := NewRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	return root
}
