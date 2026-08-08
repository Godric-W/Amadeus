package diff

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestTrackerFoldsRepeatedPatchOperations(t *testing.T) {
	root := t.TempDir()
	events := event.NewMemorySink()
	tracker, err := NewProjector(root, events)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []tool.Output{
		patchResult(operationMap("add", "a.txt", "", 3, true, false, false)),
		patchResult(operationMap("update", "a.txt", "", 5, false, false, false)),
		patchResult(operationMap("update", "a.txt", "b.txt", 5, false, false, true)),
	} {
		if err := tracker.ProjectPatch(context.Background(), result); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := tracker.Snapshot()
	if len(snapshot.Changes) != 1 || snapshot.Changes[0].Kind != ChangeAdded || snapshot.Changes[0].Path != filepath.Join(root, "b.txt") {
		t.Fatalf("unexpected folded add/move: %#v", snapshot)
	}
	if err := tracker.ProjectPatch(context.Background(), patchResult(operationMap("delete", "b.txt", "", 0, false, true, false))); err != nil {
		t.Fatal(err)
	}
	if snapshot = tracker.Snapshot(); len(snapshot.Changes) != 0 {
		t.Fatalf("add then delete should have no net change: %#v", snapshot)
	}
}

func TestTrackerFoldsMoveThenDeleteToOriginalDelete(t *testing.T) {
	root := t.TempDir()
	tracker, _ := NewProjector(root, event.NewMemorySink())
	if err := tracker.ProjectPatch(context.Background(), patchResult(operationMap("update", "old.txt", "new.txt", 7, false, false, true))); err != nil {
		t.Fatal(err)
	}
	if err := tracker.ProjectPatch(context.Background(), patchResult(operationMap("delete", "new.txt", "", 0, false, true, false))); err != nil {
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
	tracker, err := NewProjector(cwd, event.NewMemorySink())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(external, "granted.txt")
	if err := tracker.ProjectPatch(context.Background(), patchResult(operationMap("add", target, "", 7, true, false, false))); err != nil {
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
	tracker, err := NewProjector(cwd, event.NewMemorySink())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(cwd, "source.txt")
	destination := filepath.Join(external, "destination.txt")
	if err := tracker.ProjectPatch(context.Background(), patchResult(operationMap("update", source, destination, 9, false, false, true))); err != nil {
		t.Fatal(err)
	}
	snapshot := tracker.Snapshot()
	if snapshot.Invalidated || len(snapshot.Changes) != 1 || snapshot.Changes[0].Path != destination || snapshot.Changes[0].PreviousPath != source || snapshot.Changes[0].Kind != ChangeMoved {
		t.Fatalf("cross-root move was not tracked: %#v", snapshot)
	}
}

func TestProjectorOnlyConsumesExplicitPatchProjection(t *testing.T) {
	events := event.NewMemorySink()
	tracker, _ := NewProjector(t.TempDir(), events)
	if err := tracker.ProjectPatch(context.Background(), tool.Output{ToolName: "apply_patch", Metadata: map[string]any{"operations": []map[string]any{}}}); err != nil {
		t.Fatal(err)
	}
	snapshot := tracker.Snapshot()
	if snapshot.Invalidated || snapshot.Revision != 0 || len(snapshot.Changes) != 0 {
		t.Fatalf("non-Patch Tool changed exact Patch projection: %#v", snapshot)
	}
	if published := events.Snapshot(); len(published) != 0 {
		t.Fatalf("non-Patch Tool published Diff events: %#v", published)
	}
}

func TestTrackerInvalidatesMalformedPatchMetadata(t *testing.T) {
	tracker, _ := NewProjector(t.TempDir(), event.NewMemorySink())
	err := tracker.ProjectPatch(context.Background(), tool.Output{Metadata: map[string]any{"operations": "invalid"}})
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("event publication failed: %v", err)
	}
	if !tracker.Snapshot().Invalidated {
		t.Fatal("malformed exact metadata did not invalidate tracker")
	}
}

func patchResult(operations ...map[string]any) tool.Output {
	return tool.Output{ToolName: "apply_patch", Metadata: map[string]any{"operations": operations}}
}

func operationMap(kind, path, destination string, bytes int, created, deleted, moved bool) map[string]any {
	return map[string]any{"kind": kind, "path": path, "destination": destination, "bytes": bytes, "created": created, "deleted": deleted, "moved": moved}
}
