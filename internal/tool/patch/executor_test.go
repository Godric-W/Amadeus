package patch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestExecutorAppliesAddUpdateDelete(t *testing.T) {
	rootPath := t.TempDir()
	writeTestFile(t, rootPath, "existing.txt", "first\nold\nlast\n", 0o600)
	writeTestFile(t, rootPath, "delete.txt", "remove me\n", 0o644)
	executor := newTestExecutor(t, rootPath, osCommitOperations{})
	document := mustParse(t, strings.Join([]string{
		"*** Begin Patch v1",
		"*** Add File: nested/new.txt",
		"+created",
		"*** Update File: existing.txt",
		"@@",
		" first",
		"-old",
		"+new",
		" last",
		"*** Delete File: delete.txt",
		"*** End Patch",
	}, "\n"))

	result, err := executor.Apply(context.Background(), document)
	if err != nil {
		t.Fatalf("apply patch: %v", err)
	}
	if result.Partial || len(result.Applied) != 3 {
		t.Fatalf("unexpected result: %#v", result)
	}
	assertFileContent(t, rootPath, "nested/new.txt", "created\n")
	assertFileContent(t, rootPath, "existing.txt", "first\nnew\nlast\n")
	if _, err := os.Stat(filepath.Join(rootPath, "delete.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected deleted file, got %v", err)
	}
	info, err := os.Stat(filepath.Join(rootPath, "existing.txt"))
	if err != nil {
		t.Fatalf("stat updated file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("updated mode = %o, want 600", info.Mode().Perm())
	}
}

func TestExecutorPreservesCRLFAndMissingFinalNewline(t *testing.T) {
	rootPath := t.TempDir()
	writeTestFile(t, rootPath, "crlf.txt", "one\r\ntwo\r\n", 0o644)
	writeTestFile(t, rootPath, "no-newline.txt", "old", 0o644)
	executor := newTestExecutor(t, rootPath, osCommitOperations{})
	document := mustParse(t, strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: crlf.txt",
		"@@",
		" one",
		"-two",
		"+changed",
		"*** Update File: no-newline.txt",
		"@@",
		"-old",
		"+new",
		"*** End Patch",
	}, "\n"))

	if _, err := executor.Apply(context.Background(), document); err != nil {
		t.Fatalf("apply patch: %v", err)
	}
	assertFileContent(t, rootPath, "crlf.txt", "one\r\nchanged\r\n")
	assertFileContent(t, rootPath, "no-newline.txt", "new")
}

func TestExecutorDeletesBinaryRegularFile(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "binary.bin"), []byte{0, 1, 2}, 0o644); err != nil {
		t.Fatalf("write binary file: %v", err)
	}
	executor := newTestExecutor(t, rootPath, osCommitOperations{})
	document := mustParse(t, "*** Begin Patch\n*** Delete File: binary.bin\n*** End Patch")
	result, err := executor.Apply(context.Background(), document)
	if err != nil {
		t.Fatalf("delete binary file: %v", err)
	}
	if len(result.Applied) != 1 || !result.Applied[0].Deleted || !filepath.IsAbs(result.Applied[0].Path) {
		t.Fatalf("unexpected delete result: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(rootPath, "binary.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("binary file still exists: %v", err)
	}
}

func TestExecutorMovesFileWithoutAndWithUpdate(t *testing.T) {
	rootPath := t.TempDir()
	writeTestFile(t, rootPath, "plain.txt", "plain\n", 0o600)
	writeTestFile(t, rootPath, "edit.txt", "old\n", 0o640)
	executor := newTestExecutor(t, rootPath, osCommitOperations{})
	document := mustParse(t, strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: plain.txt",
		"*** Move to: moved/plain.txt",
		"*** Update File: edit.txt",
		"*** Move to: moved/edit.txt",
		"@@",
		"-old",
		"+new",
		"*** End Patch",
	}, "\n"))

	result, err := executor.Apply(context.Background(), document)
	if err != nil {
		t.Fatalf("apply move patch: %v", err)
	}
	if result.Partial || len(result.Applied) != 2 || !result.Applied[0].Moved || result.Applied[0].Destination != filepath.Join(rootPath, "moved", "plain.txt") || !result.Applied[1].Moved || result.Applied[1].Destination != filepath.Join(rootPath, "moved", "edit.txt") {
		t.Fatalf("unexpected move result: %#v", result)
	}
	assertFileContent(t, rootPath, "moved/plain.txt", "plain\n")
	assertFileContent(t, rootPath, "moved/edit.txt", "new\n")
	for _, source := range []string{"plain.txt", "edit.txt"} {
		if _, statErr := os.Stat(filepath.Join(rootPath, source)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("move source %q still exists: %v", source, statErr)
		}
	}
	info, err := os.Stat(filepath.Join(rootPath, "moved/edit.txt"))
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("move did not preserve mode: info=%v err=%v", info, err)
	}
}

func TestExecutorMoveRejectsExistingDestinationWithoutChangingSource(t *testing.T) {
	rootPath := t.TempDir()
	writeTestFile(t, rootPath, "source.txt", "source\n", 0o600)
	writeTestFile(t, rootPath, "destination.txt", "destination\n", 0o600)
	executor := newTestExecutor(t, rootPath, osCommitOperations{})
	document := mustParse(t, "*** Begin Patch\n*** Update File: source.txt\n*** Move to: destination.txt\n*** End Patch")

	if _, err := executor.Apply(context.Background(), document); err == nil || !strings.Contains(err.Error(), "destination") {
		t.Fatalf("expected destination conflict, got %v", err)
	}
	assertFileContent(t, rootPath, "source.txt", "source\n")
	assertFileContent(t, rootPath, "destination.txt", "destination\n")
}

func TestExecutorPreflightFailureLeavesAllFilesUnchanged(t *testing.T) {
	rootPath := t.TempDir()
	writeTestFile(t, rootPath, "existing.txt", "actual\n", 0o644)
	executor := newTestExecutor(t, rootPath, osCommitOperations{})
	document := mustParse(t, strings.Join([]string{
		"*** Begin Patch",
		"*** Add File: new.txt",
		"+new",
		"*** Update File: existing.txt",
		"@@",
		"-missing",
		"+changed",
		"*** End Patch",
	}, "\n"))

	result, err := executor.Apply(context.Background(), document)
	if err == nil {
		t.Fatal("expected conflict")
	}
	if result.Partial || len(result.Applied) != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Path != "existing.txt" || conflict.Matches != 0 {
		t.Fatalf("unexpected conflict: %v", err)
	}
	assertFileContent(t, rootPath, "existing.txt", "actual\n")
	if _, statErr := os.Stat(filepath.Join(rootPath, "new.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("new file should not exist: %v", statErr)
	}
}

func TestExecutorRejectsStaleBytesAndIdentityAfterPreflight(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(string) error
	}{
		{name: "bytes", mutate: func(path string) error { return os.WriteFile(path, []byte("changed\n"), 0o640) }},
		{name: "identity", mutate: func(path string) error {
			replacement := path + ".replacement"
			if err := os.WriteFile(replacement, []byte("old\n"), 0o640); err != nil {
				return err
			}
			return os.Rename(replacement, path)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			rootPath := t.TempDir()
			writeTestFile(t, rootPath, "target.txt", "old\n", 0o640)
			executor := newTestExecutor(t, rootPath, osCommitOperations{})
			document := mustParse(t, "*** Begin Patch\n*** Update File: target.txt\n@@\n-old\n+new\n*** End Patch")
			prepared, err := executor.PreparePatch(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.mutate(filepath.Join(rootPath, "target.txt")); err != nil {
				t.Fatal(err)
			}
			result, err := executor.ApplyPrepared(context.Background(), prepared)
			var conflict *ConflictError
			if !errors.As(err, &conflict) || result.Partial || len(result.Applied) != 0 {
				t.Fatalf("stale target was not rejected: result=%#v err=%v", result, err)
			}
			if content, readErr := os.ReadFile(filepath.Join(rootPath, "target.txt")); readErr != nil || string(content) == "new\n" {
				t.Fatalf("stale target was modified: content=%q err=%v", content, readErr)
			}
			assertNoPatchTemps(t, rootPath)
		})
	}
}

func TestExecutorRejectsAmbiguousHunk(t *testing.T) {
	rootPath := t.TempDir()
	writeTestFile(t, rootPath, "duplicate.txt", "same\nold\nsame\nold\n", 0o644)
	executor := newTestExecutor(t, rootPath, osCommitOperations{})
	document := mustParse(t, "*** Begin Patch\n*** Update File: duplicate.txt\n@@\n same\n-old\n+new\n*** End Patch")

	_, err := executor.Apply(context.Background(), document)
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Matches != 2 {
		t.Fatalf("expected ambiguous conflict, got %v", err)
	}
	if len(conflict.Candidates) != 2 || conflict.Candidates[0] != 1 || conflict.Candidates[1] != 3 {
		t.Fatalf("unexpected conflict candidates: %#v", conflict.Candidates)
	}
	assertFileContent(t, rootPath, "duplicate.txt", "same\nold\nsame\nold\n")
}

func TestExecutorEndOfFileAndEquivalentPunctuationMatching(t *testing.T) {
	rootPath := t.TempDir()
	writeTestFile(t, rootPath, "tail.txt", "old\nkeep\nold\n", 0o644)
	writeTestFile(t, rootPath, "punctuation.txt", "message: “old”—value\n", 0o644)
	executor := newTestExecutor(t, rootPath, osCommitOperations{})

	tail := mustParse(t, "*** Begin Patch\n*** Update File: tail.txt\n@@\n-old\n+new\n*** End of File\n*** End Patch")
	if _, err := executor.Apply(context.Background(), tail); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, rootPath, "tail.txt", "old\nkeep\nnew\n")

	punctuation := mustParse(t, "*** Begin Patch\n*** Update File: punctuation.txt\n@@\n-message: \"old\"-value\n+message: \"new\"-value\n*** End Patch")
	if _, err := executor.Apply(context.Background(), punctuation); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, rootPath, "punctuation.txt", "message: \"new\"-value\n")
}

func TestExecutorReportsPartialCommit(t *testing.T) {
	rootPath := t.TempDir()
	commits := &failingCommitOperations{failRenameAt: 2}
	executor := newTestExecutor(t, rootPath, commits)
	document := mustParse(t, strings.Join([]string{
		"*** Begin Patch",
		"*** Add File: first.txt",
		"+first",
		"*** Add File: second.txt",
		"+second",
		"*** End Patch",
	}, "\n"))

	result, err := executor.Apply(context.Background(), document)
	if err == nil {
		t.Fatal("expected commit failure")
	}
	if !result.Partial || len(result.Applied) != 1 || result.Applied[0].Path != filepath.Join(rootPath, "first.txt") {
		t.Fatalf("unexpected partial result: %#v", result)
	}
	assertFileContent(t, rootPath, "first.txt", "first\n")
	if _, statErr := os.Stat(filepath.Join(rootPath, "second.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("second file should not exist: %v", statErr)
	}
	assertNoPatchTemps(t, rootPath)
}

func TestExecutorReportsDestinationWhenMoveSourceRemovalFails(t *testing.T) {
	rootPath := t.TempDir()
	writeTestFile(t, rootPath, "source.txt", "source\n", 0o644)
	commits := &failingCommitOperations{failRemove: true}
	executor := newTestExecutor(t, rootPath, commits)
	document := mustParse(t, "*** Begin Patch\n*** Update File: source.txt\n*** Move to: destination.txt\n*** End Patch")

	result, err := executor.Apply(context.Background(), document)
	if err == nil {
		t.Fatal("expected move source removal failure")
	}
	destination := filepath.Join(rootPath, "destination.txt")
	if !result.Partial || len(result.Applied) != 1 {
		t.Fatalf("unexpected partial result: %#v", result)
	}
	applied := result.Applied[0]
	if applied.Kind != OperationAdd || applied.Path != destination || !applied.Created || applied.Moved {
		t.Fatalf("unexpected applied destination delta: %#v", applied)
	}
	assertFileContent(t, rootPath, "source.txt", "source\n")
	assertFileContent(t, rootPath, "destination.txt", "source\n")
	assertNoPatchTemps(t, rootPath)
}

func TestExecutorRejectsUnsafeAndInvalidTargets(t *testing.T) {
	rootPath := t.TempDir()
	outside := t.TempDir()
	writeTestFile(t, outside, "outside.txt", "outside\n", 0o644)
	if err := os.Symlink(filepath.Join(outside, "outside.txt"), filepath.Join(rootPath, "escape.txt")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if err := os.Mkdir(filepath.Join(rootPath, "directory"), 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	executor := newTestExecutor(t, rootPath, osCommitOperations{})

	tests := []string{
		"*** Begin Patch\n*** Add File: ../escape.txt\n+x\n*** End Patch",
		"*** Begin Patch\n*** Update File: escape.txt\n@@\n-outside\n+changed\n*** End Patch",
		"*** Begin Patch\n*** Delete File: directory\n*** End Patch",
	}
	for _, input := range tests {
		if _, err := executor.Apply(context.Background(), mustParse(t, input)); err == nil {
			t.Fatalf("expected unsafe target error for %q", input)
		}
	}
	assertFileContent(t, outside, "outside.txt", "outside\n")
}

func TestExecutorRejectsFileBudgetsBinaryAndCancellation(t *testing.T) {
	rootPath := t.TempDir()
	writeTestFile(t, rootPath, "large.txt", "12345", 0o644)
	if err := os.WriteFile(filepath.Join(rootPath, "binary.txt"), []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatalf("write binary file: %v", err)
	}
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("new root: %v", err)
	}
	executor, err := NewExecutor(root, ExecutorOptions{MaxFileBytes: 4, FileMode: 0o644})
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	large := mustParse(t, "*** Begin Patch\n*** Update File: large.txt\n@@\n-12345\n+x\n*** End Patch")
	if _, err := executor.Apply(context.Background(), large); err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("unexpected large file error: %v", err)
	}
	binary := mustParse(t, "*** Begin Patch\n*** Update File: binary.txt\n@@\n-a\n+b\n*** End Patch")
	if _, err := executor.Apply(context.Background(), binary); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("unexpected binary error: %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	add := mustParse(t, "*** Begin Patch\n*** Add File: cancelled.txt\n+x\n*** End Patch")
	if _, err := executor.Apply(cancelled, add); !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation error: %v", err)
	}
}

func TestExecutorRejectsInvalidDocument(t *testing.T) {
	executor := newTestExecutor(t, t.TempDir(), osCommitOperations{})
	tests := []struct {
		name     string
		document Document
		message  string
	}{
		{name: "version", document: Document{Version: "v2", Operations: []Operation{{Kind: OperationAdd, Path: "a"}}}, message: "unsupported"},
		{name: "duplicate", document: Document{Version: Version1, Operations: []Operation{{Kind: OperationAdd, Path: "a"}, {Kind: OperationDelete, Path: "a"}}}, message: "duplicate"},
		{name: "invalid hunk line", document: Document{Version: Version1, Operations: []Operation{{Kind: OperationUpdate, Path: "a", Hunks: []Hunk{{Lines: []Line{{Kind: "unknown", Content: "x"}}}}}}}, message: "invalid line kind"},
		{name: "nul content", document: Document{Version: Version1, Operations: []Operation{{Kind: OperationAdd, Path: "a", AddLines: []string{"a\x00b"}}}}, message: "contains NUL"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := executor.Apply(context.Background(), test.document)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("unexpected invalid document error: %v", err)
			}
		})
	}
}

type failingCommitOperations struct {
	renames      int
	failRenameAt int
	failRemove   bool
}

func (operations *failingCommitOperations) Rename(oldPath, newPath string) error {
	operations.renames++
	if operations.renames == operations.failRenameAt {
		return errors.New("injected rename failure")
	}
	return os.Rename(oldPath, newPath)
}

func (operations *failingCommitOperations) Remove(path string) error {
	if operations.failRemove {
		return errors.New("injected remove failure")
	}
	return os.Remove(path)
}

func newTestExecutor(t *testing.T, rootPath string, commitOps commitOperations) *Executor {
	t.Helper()
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("new project root: %v", err)
	}
	executor, err := newExecutor(root, ExecutorOptions{MaxFileBytes: 1 << 20, FileMode: 0o640}, commitOps)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	return executor
}

func mustParse(t *testing.T, input string) Document {
	t.Helper()
	document, err := Parse([]byte(input), ParseOptions{})
	if err != nil {
		t.Fatalf("parse patch: %v", err)
	}
	return document
}

func writeTestFile(t *testing.T, root, relative, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}

func assertFileContent(t *testing.T, root, relative, want string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	if string(content) != want {
		t.Fatalf("%s content = %q, want %q", relative, content, want)
	}
}

func assertNoPatchTemps(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".amadeus-patch-") {
			t.Fatalf("temporary patch file remains: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk root: %v", err)
	}
}
