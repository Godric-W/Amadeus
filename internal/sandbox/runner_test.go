package sandbox

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestRunnerUsesSandboxedIsolationWhenBubblewrapAvailable(t *testing.T) {
	workspace, extra, denied := t.TempDir(), t.TempDir(), t.TempDir()
	policy, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{CWD: workspace, Profile: project.PermissionProfile{ReadHost: true, WorkspaceRoots: []string{workspace, extra}, DeniedRoots: []string{denied}}})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunnerWithOptions(policy, Options{GOOS: "linux", LookPath: func(string) (string, error) { return "/usr/bin/bwrap", nil }, Probe: func(string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	launch, err := runner.Prepare("/bin/sh", "pwd", workspace)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Mode != IsolationSandboxed || launch.Executable != "/usr/bin/bwrap" || launch.Directory != string(filepath.Separator) {
		t.Fatalf("unexpected launch: %#v", launch)
	}
}

func TestRunnerUsesUnsandboxedIsolationWhenUnavailable(t *testing.T) {
	workspace := t.TempDir()
	policy, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{CWD: workspace, Profile: project.PermissionProfile{WorkspaceRoots: []string{workspace}}})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunnerWithOptions(policy, Options{GOOS: "linux", LookPath: func(string) (string, error) { return "", errors.New("missing") }})
	if err != nil {
		t.Fatal(err)
	}
	if runner.Mode() != IsolationUnsandboxed || runner.Diagnostic() == "" {
		t.Fatalf("unexpected runner: %s %q", runner.Mode(), runner.Diagnostic())
	}
}
