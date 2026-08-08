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

func TestMVPToolsReadHostAndWriteOnlyDeclaredRoots(t *testing.T) {
	primaryPath := t.TempDir()
	additional := t.TempDir()
	readOnly := t.TempDir()
	if err := os.WriteFile(filepath.Join(readOnly, "outside.txt"), []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := project.NewRoot(primaryPath)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{
		CWD: primaryPath, Profile: project.PermissionProfile{ReadHost: true, WorkspaceRoots: []string{primaryPath, additional}},
	})
	if err != nil {
		t.Fatal(err)
	}
	options := DefaultMVPOptions()
	options.FileSystemPolicy = policy
	registry, err := NewMVPRegistry(root, options)
	if err != nil {
		t.Fatal(err)
	}

	readTool, _ := registry.Lookup("read_file")
	readInput, _ := json.Marshal(map[string]any{"path": filepath.Join(readOnly, "outside.txt")})
	readResult, err := executePreparedTool(t, context.Background(), readTool, readInput)
	if err != nil || !strings.Contains(readResult.Text, "outside") {
		t.Fatalf("host read failed: %#v err=%v", readResult, err)
	}

	patchTool, _ := registry.Lookup("apply_patch")
	additionalFile := filepath.Join(additional, "created.txt")
	patchInput, _ := json.Marshal(map[string]any{"patch": "*** Begin Patch\n*** Add File: " + additionalFile + "\n+created\n*** End Patch"})
	if _, err := executePreparedTool(t, context.Background(), patchTool, patchInput); err != nil {
		t.Fatalf("additional writable root patch failed: %v", err)
	}
	if content, err := os.ReadFile(additionalFile); err != nil || string(content) != "created\n" {
		t.Fatalf("additional root content mismatch: %q err=%v", content, err)
	}

	deniedFile := filepath.Join(readOnly, "denied.txt")
	deniedInput, _ := json.Marshal(map[string]any{"patch": "*** Begin Patch\n*** Add File: " + deniedFile + "\n+denied\n*** End Patch"})
	if _, err := executePreparedTool(t, context.Background(), patchTool, deniedInput); err == nil || !strings.Contains(err.Error(), "permission_required") {
		t.Fatalf("undeclared write was not denied: %v", err)
	}
}
