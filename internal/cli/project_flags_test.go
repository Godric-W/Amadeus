package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Godric-W/Amadeus/internal/bootstrap"
)

func TestCDFlagResolvesRelativeToStartupWorkingDirectory(t *testing.T) {
	startupWorkingDirectory := t.TempDir()
	projectDirectory := filepath.Join(startupWorkingDirectory, "workspace")
	if err := os.Mkdir(projectDirectory, 0o755); err != nil {
		t.Fatalf("create project directory: %v", err)
	}

	flags := &projectFlags{}
	environment := bootstrap.Environment{
		AmadeusRoot:      t.TempDir(),
		WorkingDirectory: startupWorkingDirectory,
		LookupEnv:        emptyEnvLookup,
	}
	command := newRootCommandWithFlags(&configFlags{}, flags, RootOptions{Environment: environment})
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"version", "--cd", "workspace"})
	if err := command.Execute(); err != nil {
		t.Fatalf("parse cd flag: %v", err)
	}

	root, err := flags.resolve(command, environment)
	if err != nil {
		t.Fatalf("resolve relative project flag: %v", err)
	}
	if root.Path() != projectDirectory {
		t.Fatalf("unexpected relative project root: got %q, want %q", root.Path(), projectDirectory)
	}
}

func TestDefaultProjectUsesCapturedStartupWorkingDirectory(t *testing.T) {
	startupWorkingDirectory := t.TempDir()
	environment := bootstrap.Environment{
		AmadeusRoot:      t.TempDir(),
		WorkingDirectory: startupWorkingDirectory,
		LookupEnv:        emptyEnvLookup,
	}
	flags := &projectFlags{}
	command := newRootCommandWithFlags(&configFlags{}, flags, RootOptions{Environment: environment})

	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWorkingDirectory) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("change working directory: %v", err)
	}

	root, err := flags.resolve(command, environment)
	if err != nil {
		t.Fatalf("resolve default project: %v", err)
	}
	if root.Path() != startupWorkingDirectory {
		t.Fatalf("default project did not use startup cwd: got %q, want %q", root.Path(), startupWorkingDirectory)
	}
}

func TestAbsoluteCDDoesNotRequireStartupWorkingDirectory(t *testing.T) {
	projectDirectory := t.TempDir()
	startupErr := errors.New("working directory unavailable")
	environment := bootstrap.Environment{
		AmadeusRoot:         t.TempDir(),
		WorkingDirectoryErr: startupErr,
		LookupEnv:           emptyEnvLookup,
	}
	flags := &projectFlags{}
	command := newRootCommandWithFlags(&configFlags{}, flags, RootOptions{Environment: environment})
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"version", "-C", projectDirectory})
	if err := command.Execute(); err != nil {
		t.Fatalf("parse absolute cd flag: %v", err)
	}

	root, err := flags.resolve(command, environment)
	if err != nil {
		t.Fatalf("resolve absolute project without startup cwd: %v", err)
	}
	if root.Path() != projectDirectory {
		t.Fatalf("unexpected absolute project root: got %q, want %q", root.Path(), projectDirectory)
	}
}

func TestRelativeCDRequiresStartupWorkingDirectory(t *testing.T) {
	startupErr := errors.New("working directory unavailable")
	_, err := resolveProjectRoot("", startupErr, "workspace", true)
	if !errors.Is(err, startupErr) {
		t.Fatalf("unexpected relative project error: got %v, want %v", err, startupErr)
	}
}

func TestExplicitEmptyCDIsRejected(t *testing.T) {
	_, err := resolveProjectRoot(t.TempDir(), nil, "", true)
	if err == nil || err.Error() != "explicit working root is empty" {
		t.Fatalf("unexpected explicit empty project error: %v", err)
	}
}

func TestProjectRootIsIndependentFromAmadeusRoot(t *testing.T) {
	amadeusRoot := t.TempDir()
	projectDirectory := t.TempDir()
	environment := bootstrap.Environment{
		AmadeusRoot:      amadeusRoot,
		WorkingDirectory: projectDirectory,
		LookupEnv:        emptyEnvLookup,
	}
	flags := &projectFlags{}
	command := newRootCommandWithFlags(&configFlags{}, flags, RootOptions{Environment: environment})

	root, err := flags.resolve(command, environment)
	if err != nil {
		t.Fatalf("resolve project independently from Amadeus root: %v", err)
	}
	if root.Path() != projectDirectory {
		t.Fatalf("project root used Amadeus root: got %q, want %q", root.Path(), projectDirectory)
	}
	if root.Path() == amadeusRoot {
		t.Fatal("project root and Amadeus root were conflated")
	}
}

func TestRootCommandExposesCDFlag(t *testing.T) {
	command := newRootCommand()
	flag := command.PersistentFlags().Lookup(flagCD)
	if flag == nil {
		t.Fatal("root command does not expose --cd")
	}
	if flag.Shorthand != "C" || flag.Usage != "use the specified directory as the working root" {
		t.Fatalf("unexpected --cd contract: shorthand=%q usage=%q", flag.Shorthand, flag.Usage)
	}
	if removed := command.PersistentFlags().Lookup("project"); removed != nil {
		t.Fatal("root command retains removed --project flag")
	}
}

func TestAddDirResolvesRepeatableWritableRoots(t *testing.T) {
	startup := t.TempDir()
	primary := filepath.Join(startup, "primary")
	additional := filepath.Join(startup, "additional")
	if err := os.MkdirAll(primary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(additional, 0o755); err != nil {
		t.Fatal(err)
	}
	environment := bootstrap.Environment{WorkingDirectory: startup, LookupEnv: emptyEnvLookup}
	flags := &projectFlags{}
	command := newRootCommandWithFlags(&configFlags{}, flags, RootOptions{Environment: environment})
	command.SetArgs([]string{"version", "--cd", "primary", "--add-dir", "additional", "--add-dir", additional})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	root, err := flags.resolve(command, environment)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := flags.resolveAdditional(environment, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0] != additional {
		t.Fatalf("unexpected additional roots: %#v", resolved)
	}
}

func TestRootCommandExposesAddDirFlag(t *testing.T) {
	command := newRootCommand()
	flag := command.PersistentFlags().Lookup(flagAddDir)
	if flag == nil {
		t.Fatal("root command does not expose --add-dir")
	}
	if flag.Usage != "add an additional writable directory (repeatable)" {
		t.Fatalf("unexpected --add-dir usage: %q", flag.Usage)
	}
}
