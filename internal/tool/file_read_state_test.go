package tool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileReadStateAccumulatesOnlyMatchingSnapshotRanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paged.txt")
	firstContent := []byte("a\nb\n")
	if err := os.WriteFile(path, firstContent, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewFileReadState(path, firstContent, info, false)
	if err != nil {
		t.Fatal(err)
	}
	store := NewFileReadStateStore()
	store.RecordRange(first, 1, 1, 2)

	changedContent := []byte("x\nb\n")
	changed, err := NewFileReadState(path, changedContent, info, false)
	if err != nil {
		t.Fatal(err)
	}
	store.RecordRange(changed, 2, 2, 2)
	state, ok := store.Get(path)
	if !ok || state.FullRead || len(state.Ranges) != 1 || state.Ranges[0] != (FileReadRange{Start: 2, End: 2}) {
		t.Fatalf("changed content reused old coverage: %#v", state)
	}
	store.RecordRange(changed, 1, 1, 2)
	state, _ = store.Get(path)
	if !state.FullRead {
		t.Fatalf("matching pages did not establish full coverage: %#v", state)
	}
}
