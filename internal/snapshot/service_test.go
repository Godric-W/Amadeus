package snapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestFileServiceCapturesChangesAndRevertsRun(t *testing.T) {
	rootPath := t.TempDir()
	writeSnapshotFile(t, filepath.Join(rootPath, "keep.txt"), "before")
	writeSnapshotFile(t, filepath.Join(rootPath, "deleted.txt"), "remove me")
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewFileService(root, FileServiceOptions{Now: func() time.Time { return time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Begin(context.Background(), "run-1"); err != nil {
		t.Fatal(err)
	}
	writeSnapshotFile(t, filepath.Join(rootPath, "keep.txt"), "after")
	if err := os.Remove(filepath.Join(rootPath, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	writeSnapshotFile(t, filepath.Join(rootPath, "added.txt"), "new")
	completed, err := service.Complete(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []FileChange{{Path: "added.txt", Kind: ChangeAdded}, {Path: "deleted.txt", Kind: ChangeDeleted}, {Path: "keep.txt", Kind: ChangeModified}}
	if !reflect.DeepEqual(completed.Changes, want) {
		t.Fatalf("snapshot changes = %#v, want %#v", completed.Changes, want)
	}
	reverted, err := service.Revert(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reverted.Changes, want) {
		t.Fatalf("revert changes = %#v, want %#v", reverted.Changes, want)
	}
	assertSnapshotFile(t, filepath.Join(rootPath, "keep.txt"), "before")
	assertSnapshotFile(t, filepath.Join(rootPath, "deleted.txt"), "remove me")
	if _, err := os.Stat(filepath.Join(rootPath, "added.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("added file remained after revert: %v", err)
	}
}

func TestFileServiceRejectsUnsafeIDsAndBoundsCapture(t *testing.T) {
	rootPath := t.TempDir()
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewFileService(root, FileServiceOptions{MaxFiles: 1, MaxBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Begin(context.Background(), "../escape"); err == nil {
		t.Fatal("expected unsafe run ID rejection")
	}
	writeSnapshotFile(t, filepath.Join(rootPath, "one.txt"), "123456789")
	if _, err := service.Begin(context.Background(), "run-1"); err == nil {
		t.Fatal("expected snapshot byte budget rejection")
	}
	if _, err := service.Revert(context.Background(), "missing"); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("missing snapshot error = %v", err)
	}
}

func TestFileServiceExcludesSnapshotStorage(t *testing.T) {
	rootPath := t.TempDir()
	writeSnapshotFile(t, filepath.Join(rootPath, "file.txt"), "content")
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewFileService(root, FileServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Begin(context.Background(), "run-1"); err != nil {
		t.Fatal(err)
	}
	completed, err := service.Complete(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(completed.Changes) != 0 {
		t.Fatalf("snapshot storage became a tracked project change: %#v", completed.Changes)
	}
}

func writeSnapshotFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertSnapshotFile(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil || string(content) != want {
		t.Fatalf("file %s = %q, err=%v; want %q", path, content, err, want)
	}
}
