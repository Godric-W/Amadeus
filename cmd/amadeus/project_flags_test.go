package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectFlagResolvesRelativeToStartupWorkingDirectory(t *testing.T) {
	startupWorkingDirectory := t.TempDir()
	projectDirectory := filepath.Join(startupWorkingDirectory, "workspace")
	if err := os.Mkdir(projectDirectory, 0o755); err != nil {
		t.Fatalf("create project directory: %v", err)
	}

	flags := &projectFlags{}
	runtime := commandRuntime{
		amadeusRoot:      t.TempDir(),
		workingDirectory: startupWorkingDirectory,
		lookupEnv:        emptyEnvLookup,
	}
	command := newRootCommandWithFlags(&configFlags{}, flags, runtime)
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"version", "--project", "workspace"})
	if err := command.Execute(); err != nil {
		t.Fatalf("parse project flag: %v", err)
	}

	root, err := flags.resolve(command, runtime)
	if err != nil {
		t.Fatalf("resolve relative project flag: %v", err)
	}
	if root.Path() != projectDirectory {
		t.Fatalf("unexpected relative project root: got %q, want %q", root.Path(), projectDirectory)
	}
}

func TestDefaultProjectUsesCapturedStartupWorkingDirectory(t *testing.T) {
	startupWorkingDirectory := t.TempDir()
	runtime := commandRuntime{
		amadeusRoot:      t.TempDir(),
		workingDirectory: startupWorkingDirectory,
		lookupEnv:        emptyEnvLookup,
	}
	flags := &projectFlags{}
	command := newRootCommandWithFlags(&configFlags{}, flags, runtime)

	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWorkingDirectory) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("change working directory: %v", err)
	}

	root, err := flags.resolve(command, runtime)
	if err != nil {
		t.Fatalf("resolve default project: %v", err)
	}
	if root.Path() != startupWorkingDirectory {
		t.Fatalf("default project did not use startup cwd: got %q, want %q", root.Path(), startupWorkingDirectory)
	}
}

func TestAbsoluteProjectDoesNotRequireStartupWorkingDirectory(t *testing.T) {
	projectDirectory := t.TempDir()
	startupErr := errors.New("working directory unavailable")
	runtime := commandRuntime{
		amadeusRoot:         t.TempDir(),
		workingDirectoryErr: startupErr,
		lookupEnv:           emptyEnvLookup,
	}
	flags := &projectFlags{}
	command := newRootCommandWithFlags(&configFlags{}, flags, runtime)
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"version", "--project", projectDirectory})
	if err := command.Execute(); err != nil {
		t.Fatalf("parse absolute project flag: %v", err)
	}

	root, err := flags.resolve(command, runtime)
	if err != nil {
		t.Fatalf("resolve absolute project without startup cwd: %v", err)
	}
	if root.Path() != projectDirectory {
		t.Fatalf("unexpected absolute project root: got %q, want %q", root.Path(), projectDirectory)
	}
}

func TestRelativeProjectRequiresStartupWorkingDirectory(t *testing.T) {
	startupErr := errors.New("working directory unavailable")
	_, err := resolveProjectRoot("", startupErr, "workspace", true)
	if !errors.Is(err, startupErr) {
		t.Fatalf("unexpected relative project error: got %v, want %v", err, startupErr)
	}
}

func TestExplicitEmptyProjectIsRejected(t *testing.T) {
	_, err := resolveProjectRoot(t.TempDir(), nil, "", true)
	if err == nil || err.Error() != "explicit project path is empty" {
		t.Fatalf("unexpected explicit empty project error: %v", err)
	}
}

func TestProjectRootIsIndependentFromAmadeusRoot(t *testing.T) {
	amadeusRoot := t.TempDir()
	projectDirectory := t.TempDir()
	runtime := commandRuntime{
		amadeusRoot:      amadeusRoot,
		workingDirectory: projectDirectory,
		lookupEnv:        emptyEnvLookup,
	}
	flags := &projectFlags{}
	command := newRootCommandWithFlags(&configFlags{}, flags, runtime)

	root, err := flags.resolve(command, runtime)
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

func TestRootCommandExposesProjectFlag(t *testing.T) {
	command := newRootCommand()
	flag := command.PersistentFlags().Lookup(flagProject)
	if flag == nil {
		t.Fatal("root command does not expose --project")
	}
	if flag.Usage != "use an explicit target project directory" {
		t.Fatalf("unexpected --project usage: %q", flag.Usage)
	}
}
