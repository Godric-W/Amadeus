package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/snapshot"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestRevertTurnRestoresSnapshot(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "file.txt"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	service, err := snapshot.NewFileService(root, snapshot.FileServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Begin(context.Background(), "run-1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "file.txt"), []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(context.Background(), "run-1"); err != nil {
		t.Fatal(err)
	}
	revert, err := NewRevertTurn(service)
	if err != nil {
		t.Fatal(err)
	}
	result, err := revert.Execute(context.Background(), json.RawMessage(`{"run_id":"run-1"}`))
	if err != nil || !strings.Contains(result.Text, "reverted 1 file changes") || result.Metadata["changed_files"] != 1 {
		t.Fatalf("revert result = %#v, err=%v", result, err)
	}
	content, err := os.ReadFile(filepath.Join(rootPath, "file.txt"))
	if err != nil || string(content) != "before" {
		t.Fatalf("reverted content = %q, err=%v", content, err)
	}
	if revert.Spec().SideEffect != tool.SideEffectWrite || revert.Spec().ParallelSafe {
		t.Fatalf("unexpected revert spec: %#v", revert.Spec())
	}
}

func TestRevertTurnValidatesArguments(t *testing.T) {
	if _, err := NewRevertTurn(nil); err == nil {
		t.Fatal("expected nil service rejection")
	}
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service, err := snapshot.NewFileService(root, snapshot.FileServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	revert, err := NewRevertTurn(service)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`{"run_id":"run-1","extra":true}`)} {
		if _, err := revert.Execute(context.Background(), input); err == nil || errors.Is(err, snapshot.ErrSnapshotNotFound) {
			t.Fatalf("expected argument validation error for %s, got %v", input, err)
		}
	}
}
