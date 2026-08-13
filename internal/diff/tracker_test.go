package diff

import (
	"context"
	"path/filepath"
	"testing"

	patchtool "github.com/Godric-W/Amadeus/internal/tool/patch"
)

func TestTrackerFoldsRepeatedPatchOperations(t *testing.T) {
	root := t.TempDir()
	tracker, err := NewProjector(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, delta := range []patchtool.AppliedPatchDelta{
		patchDelta(patchtool.OperationAdd, "a.txt", "", nil, []byte("one")),
		patchDelta(patchtool.OperationUpdate, "a.txt", "", []byte("one"), []byte("three")),
		patchDelta(patchtool.OperationMove, "a.txt", "b.txt", []byte("three"), []byte("three")),
	} {
		if err := tracker.ProjectPatch(context.Background(), []patchtool.AppliedPatchDelta{delta}); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := tracker.Snapshot()
	if len(snapshot.Changes) != 1 || snapshot.Changes[0].Kind != ChangeAdded || snapshot.Changes[0].Path != filepath.Join(root, "b.txt") {
		t.Fatalf("unexpected folded add/move: %#v", snapshot)
	}
	if err := tracker.ProjectPatch(context.Background(), []patchtool.AppliedPatchDelta{patchDelta(patchtool.OperationDelete, "b.txt", "", []byte("three"), nil)}); err != nil {
		t.Fatal(err)
	}
	if snapshot = tracker.Snapshot(); len(snapshot.Changes) != 0 {
		t.Fatalf("add then delete should have no net change: %#v", snapshot)
	}
}

func TestTrackerFoldsMoveThenDeleteToOriginalDelete(t *testing.T) {
	root := t.TempDir()
	tracker, _ := NewProjector(root)
	if err := tracker.ProjectPatch(context.Background(), []patchtool.AppliedPatchDelta{patchDelta(patchtool.OperationMove, "old.txt", "new.txt", []byte("content"), []byte("content"))}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.ProjectPatch(context.Background(), []patchtool.AppliedPatchDelta{patchDelta(patchtool.OperationDelete, "new.txt", "", []byte("content"), nil)}); err != nil {
		t.Fatal(err)
	}
	snapshot := tracker.Snapshot()
	if len(snapshot.Changes) != 1 || snapshot.Changes[0].Kind != ChangeDeleted || snapshot.Changes[0].Path != filepath.Join(root, "old.txt") {
		t.Fatalf("unexpected move/delete fold: %#v", snapshot)
	}
}

func TestTrackerTracksAbsolutePathsAcrossWritableRoots(t *testing.T) {
	cwd := t.TempDir()
	external := t.TempDir()
	tracker, err := NewProjector(cwd)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(external, "granted.txt")
	if err := tracker.ProjectPatch(context.Background(), []patchtool.AppliedPatchDelta{patchDelta(patchtool.OperationAdd, target, "", nil, []byte("content"))}); err != nil {
		t.Fatal(err)
	}
	snapshot := tracker.Snapshot()
	if snapshot.Invalidated || len(snapshot.Changes) != 1 || snapshot.Changes[0].Path != target || snapshot.Changes[0].Kind != ChangeAdded {
		t.Fatalf("absolute writable-root change was not tracked: %#v", snapshot)
	}
}

func TestTrackerTracksMoveAcrossWritableRoots(t *testing.T) {
	cwd := t.TempDir()
	external := t.TempDir()
	tracker, err := NewProjector(cwd)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(cwd, "source.txt")
	destination := filepath.Join(external, "destination.txt")
	if err := tracker.ProjectPatch(context.Background(), []patchtool.AppliedPatchDelta{patchDelta(patchtool.OperationMove, source, destination, []byte("content!!"), []byte("content!!"))}); err != nil {
		t.Fatal(err)
	}
	snapshot := tracker.Snapshot()
	if snapshot.Invalidated || len(snapshot.Changes) != 1 || snapshot.Changes[0].Path != destination || snapshot.Changes[0].PreviousPath != source || snapshot.Changes[0].Kind != ChangeMoved {
		t.Fatalf("cross-root move was not tracked: %#v", snapshot)
	}
}

func TestProjectorOnlyConsumesExplicitPatchProjection(t *testing.T) {
	tracker, _ := NewProjector(t.TempDir())
	if err := tracker.ProjectPatch(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	snapshot := tracker.Snapshot()
	if snapshot.Invalidated || snapshot.Revision != 0 || len(snapshot.Changes) != 0 {
		t.Fatalf("non-Patch Tool changed exact Patch projection: %#v", snapshot)
	}
}

func TestTrackerInvalidatesInexactPatchDelta(t *testing.T) {
	tracker, _ := NewProjector(t.TempDir())
	delta := patchDelta(patchtool.OperationUpdate, "a.txt", "", []byte("before"), []byte("after"))
	delta.Exact = false
	if err := tracker.ProjectPatch(context.Background(), []patchtool.AppliedPatchDelta{delta}); err != nil {
		t.Fatalf("inexact Patch delta was not rejected: %v", err)
	}
	snapshot := tracker.Snapshot()
	if !snapshot.Invalidated || len(snapshot.Changes) != 0 {
		t.Fatalf("inexact Patch delta was not rejected: %#v", snapshot)
	}
}

func patchDelta(operation patchtool.OperationKind, path, destination string, oldContent, newContent []byte) patchtool.AppliedPatchDelta {
	return patchtool.AppliedPatchDelta{
		Path: path, Destination: destination, Operation: operation,
		OldContent: oldContent, NewContent: newContent,
		UnifiedDiff: "--- old\n+++ new\n@@\n", Exact: true,
	}
}
