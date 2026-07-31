package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
	patchtool "github.com/Godric-W/Amadeus/internal/tool/patch"
)

func TestApplyPatchExecutesDocumentAndReturnsStructuredResult(t *testing.T) {
	rootPath := t.TempDir()
	writeBuiltinPatchFile(t, rootPath, "update.txt", "old\n")
	writeBuiltinPatchFile(t, rootPath, "delete.bin", "\x00binary")
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	candidate, err := NewApplyPatch(root, ApplyPatchOptions{Executor: patchtool.ExecutorOptions{MaxFileBytes: 1 << 20, FileMode: 0o644}})
	if err != nil {
		t.Fatalf("create apply_patch tool: %v", err)
	}
	patch := "*** Begin Patch v1\n" +
		"*** Update File: update.txt\n@@\n-old\n+new\n" +
		"*** Add File: add.txt\n+added\n" +
		"*** Delete File: delete.bin\n*** End Patch\n"
	input, err := json.Marshal(map[string]string{"patch": patch})
	if err != nil {
		t.Fatalf("encode arguments: %v", err)
	}

	result, err := candidate.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute apply_patch: %v", err)
	}
	if result.ToolName != "apply_patch" || result.Partial || result.Text != "applied 3 patch operation(s)" {
		t.Fatalf("unexpected apply_patch result: %#v", result)
	}
	if result.Metadata["operation_count"] != 3 || result.Metadata["total_operations"] != 3 || result.Metadata["partial"] != false {
		t.Fatalf("unexpected apply_patch metadata: %#v", result.Metadata)
	}
	operations, ok := result.Metadata["operations"].([]map[string]any)
	if !ok || len(operations) != 3 || operations[0]["kind"] != patchtool.OperationUpdate || operations[1]["created"] != true || operations[2]["deleted"] != true {
		t.Fatalf("unexpected operation metadata: %#v", result.Metadata["operations"])
	}
	assertBuiltinPatchContent(t, rootPath, "update.txt", "new\n")
	assertBuiltinPatchContent(t, rootPath, "add.txt", "added\n")
	if _, err := os.Stat(filepath.Join(rootPath, "delete.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted file still exists: %v", err)
	}
}

func TestApplyPatchPreservesPartialExecutorResult(t *testing.T) {
	expected := errors.New("second commit failed")
	applier := &fakePatchApplier{result: patchtool.ApplyResult{
		Applied: []patchtool.OperationResult{{Kind: patchtool.OperationUpdate, Path: "first.txt", Bytes: 4}},
		Partial: true,
	}, err: expected}
	candidate, err := newApplyPatch(ApplyPatchOptions{}, applier)
	if err != nil {
		t.Fatalf("create fake apply_patch tool: %v", err)
	}
	input := json.RawMessage(`{"patch":"*** Begin Patch v1\n*** Update File: first.txt\n@@\n-old\n+new\n*** Delete File: second.txt\n*** End Patch\n"}`)

	result, err := candidate.Execute(context.Background(), input)
	if !errors.Is(err, expected) {
		t.Fatalf("unexpected partial error: %v", err)
	}
	if !result.Partial || result.Text != "applied 1 of 2 patch operation(s) before failure" || result.Metadata["operation_count"] != 1 || result.Metadata["total_operations"] != 2 {
		t.Fatalf("partial result was not preserved: %#v", result)
	}
}

func TestApplyPatchHonorsPreCancelledContext(t *testing.T) {
	applier := &fakePatchApplier{}
	candidate, err := newApplyPatch(ApplyPatchOptions{}, applier)
	if err != nil {
		t.Fatalf("create fake apply_patch tool: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := candidate.Execute(ctx, json.RawMessage(`{"patch":"*** Begin Patch v1\n*** Add File: file.txt\n+x\n*** End Patch\n"}`))
	if !errors.Is(err, context.Canceled) || applier.calls != 0 || result.CallID != "" || result.ToolName != "" || result.Text != "" || len(result.Parts) != 0 || result.Metadata != nil || result.Partial {
		t.Fatalf("unexpected cancelled execution: result=%#v calls=%d err=%v", result, applier.calls, err)
	}
}

func TestApplyPatchRejectsInvalidDocumentBeforeExecution(t *testing.T) {
	applier := &fakePatchApplier{}
	candidate, err := newApplyPatch(ApplyPatchOptions{}, applier)
	if err != nil {
		t.Fatalf("create fake apply_patch tool: %v", err)
	}
	if _, err := candidate.Execute(context.Background(), json.RawMessage(`{"patch":"not a patch"}`)); err == nil || applier.calls != 0 {
		t.Fatalf("invalid patch reached executor: calls=%d err=%v", applier.calls, err)
	}
}

type fakePatchApplier struct {
	result patchtool.ApplyResult
	err    error
	calls  int
}

func (applier *fakePatchApplier) Apply(context.Context, patchtool.Document) (patchtool.ApplyResult, error) {
	applier.calls++
	return applier.result, applier.err
}

func writeBuiltinPatchFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", relative, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

func assertBuiltinPatchContent(t *testing.T, root, relative, want string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	if string(content) != want {
		t.Fatalf("%s content = %q, want %q", relative, content, want)
	}
}
