package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
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

	result, err := writeFile.Execute(context.Background(), json.RawMessage(`{"path":"nested/file.txt","content":"first","mode":"create"}`))
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if result.Metadata["created"] != true || result.Metadata["mode"] != WriteFileModeCreate || result.Metadata["atomic"] != true {
		t.Fatalf("unexpected create metadata: %#v", result.Metadata)
	}
	if err := os.Chmod(filepath.Join(rootPath, "nested", "file.txt"), 0o600); err != nil {
		t.Fatalf("set existing permissions: %v", err)
	}
	if _, err := writeFile.Execute(context.Background(), json.RawMessage(`{"path":"nested/file.txt","content":"second","mode":"replace"}`)); err != nil {
		t.Fatalf("replace file: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(rootPath, "nested", "file.txt"))
	if err != nil || string(content) != "second" {
		t.Fatalf("unexpected replaced content: %q, %v", content, err)
	}
	info, err := os.Stat(filepath.Join(rootPath, "nested", "file.txt"))
	if err != nil {
		t.Fatalf("stat replaced file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("replace did not preserve permissions: mode=%v", info.Mode())
	}
	entries, err := os.ReadDir(filepath.Join(rootPath, "nested"))
	if err != nil || len(entries) != 1 || strings.HasPrefix(entries[0].Name(), ".amadeus-write-") {
		t.Fatalf("temporary file leaked: entries=%#v err=%v", entries, err)
	}
}

func TestWriteFileEnforcesExplicitCreateAndReplaceModes(t *testing.T) {
	rootPath := t.TempDir()
	writeFile := newTestWriteFile(t, rootPath, 1024)
	path := filepath.Join(rootPath, "existing.txt")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{name: "implicit mode", input: `{"path":"implicit.txt","content":"x"}`, contains: "mode is required"},
		{name: "invalid mode", input: `{"path":"invalid.txt","content":"x","mode":"upsert"}`, contains: "mode \"upsert\" is unsupported"},
		{name: "create existing", input: `{"path":"existing.txt","content":"changed","mode":"create"}`, contains: "create target already exists"},
		{name: "replace missing", input: `{"path":"missing/target.txt","content":"changed","mode":"replace"}`, contains: "replace target does not exist"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := writeFile.Execute(context.Background(), json.RawMessage(test.input)); err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("unexpected mode error: %v", err)
			}
		})
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "original" {
		t.Fatalf("rejected mode changed existing file: content=%q err=%v", content, err)
	}
	if _, err := os.Stat(filepath.Join(rootPath, "implicit.txt")); !os.IsNotExist(err) {
		t.Fatalf("implicit mode created a file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootPath, "missing")); !os.IsNotExist(err) {
		t.Fatalf("replace missing created parent directories: %v", err)
	}
}

func TestWriteFileSchemaRejectsLegacyArgumentsWithoutMode(t *testing.T) {
	validator := tool.NewArgumentValidator()
	if _, err := validator.Validate(writeFileSpec(), json.RawMessage(`{"path":"legacy.txt","content":"x"}`)); err == nil {
		t.Fatal("legacy write_file arguments without mode passed schema validation")
	}
	for _, mode := range []string{"create", "replace"} {
		input := json.RawMessage(`{"path":"file.txt","content":"x","mode":"` + mode + `"}`)
		if _, err := validator.Validate(writeFileSpec(), input); err != nil {
			t.Fatalf("valid write_file mode %q failed schema validation: %v", mode, err)
		}
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
		{context.Background(), `{"path":"large.txt","content":"12345","mode":"create"}`},
		{context.Background(), `{"path":"../outside","content":"ok","mode":"create"}`},
		{ctx, `{"path":"cancelled.txt","content":"ok","mode":"create"}`},
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
