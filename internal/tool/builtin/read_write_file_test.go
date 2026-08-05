package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestReadFileSupportsOneBasedLineLimitAndStableMetadata(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "notes.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	readFile := newTestReadFile(t, rootPath, 1024)

	result, err := readFile.Execute(context.Background(), json.RawMessage(`{"path":"notes.txt","line":2,"limit":1}`))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if result.Text != "L2:two\n" || !result.Partial || result.Metadata["total_lines"] != 3 || result.Metadata["lines_returned"] != 1 || result.Metadata["next_line"] != 3 {
		t.Fatalf("unexpected read result: %#v", result)
	}
}

func TestReadFileStreamsOversizeFileAndRejectsBinaryAndEscape(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "large.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("write large fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "binary.bin"), []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatalf("write binary fixture: %v", err)
	}
	readFile := newTestReadFile(t, rootPath, 16)
	result, err := readFile.Execute(context.Background(), json.RawMessage(`{"path":"large.txt","line":2,"limit":1}`))
	if err != nil || result.Text != "L2:two\n" || result.Metadata["file_bytes"] != int64(14) {
		t.Fatalf("large file range read failed: result=%#v err=%v", result, err)
	}
	for _, input := range []string{`{"path":"binary.bin"}`, `{"path":"../outside"}`} {
		if _, err := readFile.Execute(context.Background(), json.RawMessage(input)); err == nil {
			t.Fatalf("expected read rejection for %s", input)
		}
	}
}

func TestReadFileTruncatesLongLinesWithinOutputBudget(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "long.txt"), []byte(strings.Repeat("界", 20)+"\nnext\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	readFile, err := NewReadFile(root, ReadFileOptions{MaxBytes: 64, MaxLineBytes: 12})
	if err != nil {
		t.Fatal(err)
	}
	result, err := readFile.Execute(context.Background(), json.RawMessage(`{"path":"long.txt","limit":1}`))
	if err != nil || !strings.HasPrefix(result.Text, "L1:") || result.Metadata["lines_truncated"] != 1 || result.Metadata["next_line"] != 2 {
		t.Fatalf("unexpected truncated line result: result=%#v err=%v", result, err)
	}
}

func newTestReadFile(t *testing.T, rootPath string, maxBytes int64) *ReadFile {
	t.Helper()
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	readFile, err := NewReadFile(root, ReadFileOptions{MaxBytes: maxBytes})
	if err != nil {
		t.Fatalf("create read_file: %v", err)
	}
	return readFile
}
