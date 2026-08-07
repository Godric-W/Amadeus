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
		name      string
		tool      tool.Tool
		input     string
		wantError string
	}{
		{name: "read_file", tool: mustReadFile(t, root, options.ReadFile), input: `{"path":"escape/secret.txt"}`, wantError: "path_denied"},
		{name: "apply_patch", tool: mustApplyPatch(t, root, options.ApplyPatch), input: `{"patch":"*** Begin Patch\n*** Add File: escape/new.txt\n+blocked\n*** End Patch"}`, wantError: "permission_required"},
		{name: "list_dir", tool: mustListDir(t, root, options.ListDir), input: `{"path":"escape"}`, wantError: "path_denied"},
		{name: "glob_files", tool: mustGlobFiles(t, root, options.GlobFiles), input: `{"pattern":"**"}`, wantError: "path_denied"},
		{name: "grep_code", tool: mustGrepCode(t, root, options.GrepCode), input: `{"query":"secret","path":"escape"}`, wantError: "path_denied"},
		{name: "execute_command", tool: mustExecuteCommand(t, root, options.ExecuteCommand), input: `{"command":"pwd","cwd":"escape"}`, wantError: "path_denied"},
	}
	for _, test := range tools {
		t.Run(test.name, func(t *testing.T) {
			result, err := executePreparedTool(t, context.Background(), test.tool, json.RawMessage(test.input))
			if test.wantError == "" && err != nil {
				t.Fatalf("unexpected result: result=%#v err=%v", result, err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("unexpected external write result: result=%#v err=%v", result, err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(external, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("apply_patch modified escaped target: %v", err)
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
	readResult, err := executePreparedTool(t, context.Background(), mustReadFile(t, root, options.ReadFile), json.RawMessage(`{"path":"alias/file.txt"}`))
	if err != nil || readResult.Text != "L1:inside" {
		t.Fatalf("unexpected internal symlink read: result=%#v err=%v", readResult, err)
	}
	if _, err := executePreparedTool(t, context.Background(), mustApplyPatch(t, root, options.ApplyPatch), json.RawMessage(`{"patch":"*** Begin Patch\n*** Add File: alias/new.txt\n+new\n*** End Patch"}`)); err != nil {
		t.Fatalf("patch through internal directory symlink: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(realDirectory, "new.txt"))
	if err != nil || string(content) != "new\n" {
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

func mustApplyPatch(t *testing.T, root project.Root, options ApplyPatchOptions) *ApplyPatch {
	t.Helper()
	value, err := NewApplyPatch(root, options)
	if err != nil {
		t.Fatalf("create apply_patch: %v", err)
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
