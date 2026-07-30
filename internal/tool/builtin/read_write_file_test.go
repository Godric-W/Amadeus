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

func TestReadFileSupportsOffsetLimitAndStableMetadata(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "notes.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	readFile := newTestReadFile(t, rootPath, 1024)

	result, err := readFile.Execute(context.Background(), json.RawMessage(`{"path":"notes.txt","offset":1,"limit":1}`))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if result.Text != "two\n" || !result.Partial || result.Metadata["total_lines"] != 3 || result.Metadata["lines_returned"] != 1 {
		t.Fatalf("unexpected read result: %#v", result)
	}
}

func TestReadFileRejectsOversizeBinaryAndEscape(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "large.txt"), []byte("12345"), 0o644); err != nil {
		t.Fatalf("write large fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "binary.bin"), []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatalf("write binary fixture: %v", err)
	}
	readFile := newTestReadFile(t, rootPath, 4)
	for _, input := range []string{`{"path":"large.txt"}`, `{"path":"binary.bin"}`, `{"path":"../outside"}`} {
		if _, err := readFile.Execute(context.Background(), json.RawMessage(input)); err == nil {
			t.Fatalf("expected read rejection for %s", input)
		}
	}
}

func TestWriteFileCreatesParentsAndAtomicallyReplaces(t *testing.T) {
	rootPath := t.TempDir()
	writeFile := newTestWriteFile(t, rootPath, 1024)

	result, err := writeFile.Execute(context.Background(), json.RawMessage(`{"path":"nested/file.txt","content":"first"}`))
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if result.Metadata["created"] != true || result.Metadata["atomic"] != true {
		t.Fatalf("unexpected create metadata: %#v", result.Metadata)
	}
	if _, err := writeFile.Execute(context.Background(), json.RawMessage(`{"path":"nested/file.txt","content":"second"}`)); err != nil {
		t.Fatalf("replace file: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(rootPath, "nested", "file.txt"))
	if err != nil || string(content) != "second" {
		t.Fatalf("unexpected replaced content: %q, %v", content, err)
	}
	entries, err := os.ReadDir(filepath.Join(rootPath, "nested"))
	if err != nil || len(entries) != 1 || strings.HasPrefix(entries[0].Name(), ".amadeus-write-") {
		t.Fatalf("temporary file leaked: entries=%#v err=%v", entries, err)
	}
}

func TestWriteFileRejectsOversizeEscapeAndCancelledContext(t *testing.T) {
	rootPath := t.TempDir()
	writeFile := newTestWriteFile(t, rootPath, 4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inputs := []struct {
		ctx   context.Context
		input string
	}{
		{context.Background(), `{"path":"large.txt","content":"12345"}`},
		{context.Background(), `{"path":"../outside","content":"ok"}`},
		{ctx, `{"path":"cancelled.txt","content":"ok"}`},
	}
	for _, test := range inputs {
		if _, err := writeFile.Execute(test.ctx, json.RawMessage(test.input)); err == nil {
			t.Fatalf("expected write rejection for %s", test.input)
		}
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

func newTestWriteFile(t *testing.T, rootPath string, maxBytes int64) *WriteFile {
	t.Helper()
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	writeFile, err := NewWriteFile(root, WriteFileOptions{MaxBytes: maxBytes})
	if err != nil {
		t.Fatalf("create write_file: %v", err)
	}
	return writeFile
}
