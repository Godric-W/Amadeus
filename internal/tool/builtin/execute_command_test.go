package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestExecuteCommandUsesFixedProjectCWDAndCombinedOutput(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootPath, "sub"), 0o755); err != nil {
		t.Fatalf("create subdirectory: %v", err)
	}
	executeCommand := newTestExecuteCommand(t, rootPath, 5*time.Second, 1024, 100)
	result, err := executePreparedTool(t, context.Background(), executeCommand, json.RawMessage(`{"command":"printf 'out\\n'; printf 'err\\n' >&2; pwd","cwd":"sub"}`))
	if err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if !strings.HasPrefix(result.Text, "out\nerr\n") || !strings.Contains(result.Text, filepath.Join(rootPath, "sub")) || result.Metadata["exit_code"] != 0 {
		t.Fatalf("unexpected command result: %#v", result)
	}
}

func TestExecuteCommandReturnsOutputWithExitError(t *testing.T) {
	executeCommand := newTestExecuteCommand(t, t.TempDir(), 5*time.Second, 1024, 100)
	result, err := executePreparedTool(t, context.Background(), executeCommand, json.RawMessage(`{"command":"printf failure; exit 7"}`))
	var exitError *CommandExitError
	if !errors.As(err, &exitError) || exitError.ExitCode != 7 || result.Text != "failure" || result.Metadata["exit_code"] != 7 {
		t.Fatalf("unexpected exit failure: result=%#v err=%v", result, err)
	}
}

func TestExecuteCommandTimeoutAndCancellationArePartial(t *testing.T) {
	executeCommand := newTestExecuteCommand(t, t.TempDir(), 2*time.Second, 1024, 100)
	startedAt := time.Now()
	result, err := executePreparedTool(t, context.Background(), executeCommand, json.RawMessage(`{"command":"printf started; sleep 5","timeout_ms":30}`))
	if !errors.Is(err, ErrCommandTimeout) || !result.Partial || result.Metadata["timed_out"] != true || time.Since(startedAt) > time.Second {
		t.Fatalf("unexpected timeout result: result=%#v err=%v elapsed=%s", result, err, time.Since(startedAt))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = executePreparedTool(t, ctx, executeCommand, json.RawMessage(`{"command":"printf ignored"}`))
	if !errors.Is(err, context.Canceled) || result.CallID != "test-call" || result.ToolName != "execute_command" || result.Text != context.Canceled.Error() {
		t.Fatalf("unexpected pre-cancel result: result=%#v err=%v", result, err)
	}
}

func TestExecuteCommandAppliesByteAndLineOutputBudget(t *testing.T) {
	executeCommand := newTestExecuteCommand(t, t.TempDir(), 5*time.Second, 8, 2)
	result, err := executePreparedTool(t, context.Background(), executeCommand, json.RawMessage(`{"command":"printf 'one\\ntwo\\nthree\\n'"}`))
	if err != nil {
		t.Fatalf("execute bounded command: %v", err)
	}
	if !result.Partial || !strings.Contains(result.Text, "one\n") || !strings.Contains(result.Text, "ree\n") || !strings.Contains(result.Text, "output truncated") || result.Metadata["output_bytes"] != int64(14) || result.Metadata["output_truncated"] != true {
		t.Fatalf("unexpected bounded output: %#v", result)
	}
}

func TestExecuteCommandAllowsReadableExternalCWD(t *testing.T) {
	rootPath := t.TempDir()
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	filesystemPolicy, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{CWD: rootPath, Profile: project.PermissionProfile{ReadHost: true, WorkspaceRoots: []string{rootPath}}})
	if err != nil {
		t.Fatal(err)
	}
	executeCommand, err := NewExecuteCommand(root, ExecuteCommandOptions{DefaultTimeout: 5 * time.Second, MaxTimeout: 5 * time.Second, MaxOutputBytes: 1024, MaxOutputLines: 100, FileSystemPolicy: filesystemPolicy})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executePreparedTool(t, context.Background(), executeCommand, json.RawMessage(`{"command":"pwd","cwd":".."}`)); err != nil {
		t.Fatalf("external readable cwd rejected: %v", err)
	}
}

func newTestExecuteCommand(t *testing.T, rootPath string, maxTimeout time.Duration, maxBytes int64, maxLines int) *ExecuteCommand {
	t.Helper()
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	executeCommand, err := NewExecuteCommand(root, ExecuteCommandOptions{
		DefaultTimeout: maxTimeout, MaxTimeout: maxTimeout, MaxOutputBytes: maxBytes, MaxOutputLines: maxLines,
	})
	if err != nil {
		t.Fatalf("create execute_command: %v", err)
	}
	return executeCommand
}
