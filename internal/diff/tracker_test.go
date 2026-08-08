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
	tracker, err := NewTracker(root, events)
	if err != nil {
		t.Fatal(err)
	}
	apply := tool.Spec{Name: "apply_patch"}
	for _, result := range []tool.Result{
		patchResult(operationMap("add", "a.txt", "", 3, true, false, false)),
		patchResult(operationMap("update", "a.txt", "", 5, false, false, false)),
		patchResult(operationMap("update", "a.txt", "b.txt", 5, false, false, true)),
	} {
		if err := tracker.After(context.Background(), apply, tool.Call{}, result); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := tracker.Snapshot()
	if len(snapshot.Changes) != 1 || snapshot.Changes[0].Kind != ChangeAdded || snapshot.Changes[0].Path != filepath.Join(root, "b.txt") {
		t.Fatalf("unexpected folded add/move: %#v", snapshot)
	}
	if err := tracker.After(context.Background(), apply, tool.Call{}, patchResult(operationMap("delete", "b.txt", "", 0, false, true, false))); err != nil {
		t.Fatal(err)
	}
	if snapshot = tracker.Snapshot(); len(snapshot.Changes) != 0 {
		t.Fatalf("add then delete should have no net change: %#v", snapshot)
	}
}

func TestTrackerFoldsMoveThenDeleteToOriginalDelete(t *testing.T) {
	root := t.TempDir()
	tracker, _ := NewTracker(root, event.NewMemorySink())
	apply := tool.Spec{Name: "apply_patch"}
	if err := tracker.After(context.Background(), apply, tool.Call{}, patchResult(operationMap("update", "old.txt", "new.txt", 7, false, false, true))); err != nil {
		t.Fatal(err)
	}
	if err := tracker.After(context.Background(), apply, tool.Call{}, patchResult(operationMap("delete", "new.txt", "", 0, false, true, false))); err != nil {
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
	tracker, err := NewTracker(cwd, event.NewMemorySink())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(external, "granted.txt")
	if err := tracker.After(context.Background(), tool.Spec{Name: "apply_patch"}, tool.Call{}, patchResult(operationMap("add", target, "", 7, true, false, false))); err != nil {
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
	tracker, err := NewTracker(cwd, event.NewMemorySink())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(cwd, "source.txt")
	destination := filepath.Join(external, "destination.txt")
	if err := tracker.After(context.Background(), tool.Spec{Name: "apply_patch"}, tool.Call{}, patchResult(operationMap("update", source, destination, 9, false, false, true))); err != nil {
		t.Fatal(err)
	}
	snapshot := tracker.Snapshot()
	if snapshot.Invalidated || len(snapshot.Changes) != 1 || snapshot.Changes[0].Path != destination || snapshot.Changes[0].PreviousPath != source || snapshot.Changes[0].Kind != ChangeMoved {
		t.Fatalf("cross-root move was not tracked: %#v", snapshot)
	}
}

func TestTrackerIgnoresToolsWithoutExactPatchDelta(t *testing.T) {
	events := event.NewMemorySink()
	tracker, _ := NewTracker(t.TempDir(), events)
	for _, name := range []string{"execute_command", "write_stdin", "mcp_call", "read_file"} {
		if err := tracker.After(context.Background(), tool.Spec{Name: name}, tool.Call{}, tool.Result{}); err != nil {
			t.Fatalf("ignore %s: %v", name, err)
		}
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
	tracker, _ := NewTracker(t.TempDir(), event.NewMemorySink())
	err := tracker.After(context.Background(), tool.Spec{Name: "apply_patch"}, tool.Call{}, tool.Result{Metadata: map[string]any{"operations": "invalid"}})
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("event publication failed: %v", err)
	}
	if !tracker.Snapshot().Invalidated {
		t.Fatal("malformed exact metadata did not invalidate tracker")
	}
}

func patchResult(operations ...map[string]any) tool.Result {
	return tool.Result{ToolName: "apply_patch", Metadata: map[string]any{"operations": operations}}
}

func operationMap(kind, path, destination string, bytes int, created, deleted, moved bool) map[string]any {
	return map[string]any{"kind": kind, "path": path, "destination": destination, "bytes": bytes, "created": created, "deleted": deleted, "moved": moved}
}
