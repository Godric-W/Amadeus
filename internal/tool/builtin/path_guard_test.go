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

func TestMVPToolsRejectSymlinkEscapes(t *testing.T) {
	rootPath := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("write external secret: %v", err)
	}
	if err := os.Symlink(external, filepath.Join(rootPath, "escape")); err != nil {
		t.Fatalf("create escaping symlink: %v", err)
	}
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	options := DefaultMVPOptions()
	options.GrepCode.DisableRipgrep = true
	tools := []struct {
		name  string
		tool  tool.Tool
		input string
	}{
		{name: "read_file", tool: mustReadFile(t, root, options.ReadFile), input: `{"path":"escape/secret.txt"}`},
		{name: "write_file", tool: mustWriteFile(t, root, options.WriteFile), input: `{"path":"escape/new.txt","content":"blocked"}`},
		{name: "list_dir", tool: mustListDir(t, root, options.ListDir), input: `{"path":"escape"}`},
		{name: "glob_files", tool: mustGlobFiles(t, root, options.GlobFiles), input: `{"pattern":"**"}`},
		{name: "grep_code", tool: mustGrepCode(t, root, options.GrepCode), input: `{"query":"secret","path":"escape"}`},
		{name: "execute_command", tool: mustExecuteCommand(t, root, options.ExecuteCommand), input: `{"command":"pwd","cwd":"escape"}`},
	}
	for _, test := range tools {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.tool.Execute(context.Background(), json.RawMessage(test.input))
			if err == nil || !strings.Contains(err.Error(), "outside project root") || result.ToolName != "" {
				t.Fatalf("unexpected symlink escape result: result=%#v err=%v", result, err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(external, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("write_file modified escaped target: %v", err)
	}
}

func TestMVPFileToolsAllowInternalDirectorySymlink(t *testing.T) {
	rootPath := t.TempDir()
	realDirectory := filepath.Join(rootPath, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatalf("create real directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realDirectory, "file.txt"), []byte("inside"), 0o600); err != nil {
		t.Fatalf("write internal file: %v", err)
	}
	if err := os.Symlink(realDirectory, filepath.Join(rootPath, "alias")); err != nil {
		t.Fatalf("create internal symlink: %v", err)
	}
	root, _ := project.NewRoot(rootPath)
	options := DefaultMVPOptions()
	readResult, err := mustReadFile(t, root, options.ReadFile).Execute(context.Background(), json.RawMessage(`{"path":"alias/file.txt"}`))
	if err != nil || readResult.Text != "inside" {
		t.Fatalf("unexpected internal symlink read: result=%#v err=%v", readResult, err)
	}
	if _, err := mustWriteFile(t, root, options.WriteFile).Execute(context.Background(), json.RawMessage(`{"path":"alias/new.txt","content":"new"}`)); err != nil {
		t.Fatalf("write through internal directory symlink: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(realDirectory, "new.txt"))
	if err != nil || string(content) != "new" {
		t.Fatalf("unexpected internal symlink write: content=%q err=%v", content, err)
	}
}

func mustReadFile(t *testing.T, root project.Root, options ReadFileOptions) *ReadFile {
	t.Helper()
	value, err := NewReadFile(root, options)
	if err != nil {
		t.Fatalf("create read_file: %v", err)
	}
	return value
}

func mustWriteFile(t *testing.T, root project.Root, options WriteFileOptions) *WriteFile {
	t.Helper()
	value, err := NewWriteFile(root, options)
	if err != nil {
		t.Fatalf("create write_file: %v", err)
	}
	return value
}

func mustListDir(t *testing.T, root project.Root, options ListDirOptions) *ListDir {
	t.Helper()
	value, err := NewListDir(root, options)
	if err != nil {
		t.Fatalf("create list_dir: %v", err)
	}
	return value
}

func mustGlobFiles(t *testing.T, root project.Root, options GlobFilesOptions) *GlobFiles {
	t.Helper()
	value, err := NewGlobFiles(root, options)
	if err != nil {
		t.Fatalf("create glob_files: %v", err)
	}
	return value
}

func mustGrepCode(t *testing.T, root project.Root, options GrepCodeOptions) *GrepCode {
	t.Helper()
	value, err := NewGrepCode(root, options)
	if err != nil {
		t.Fatalf("create grep_code: %v", err)
	}
	return value
}

func mustExecuteCommand(t *testing.T, root project.Root, options ExecuteCommandOptions) *ExecuteCommand {
	t.Helper()
	value, err := NewExecuteCommand(root, options)
	if err != nil {
		t.Fatalf("create execute_command: %v", err)
	}
	return value
}
