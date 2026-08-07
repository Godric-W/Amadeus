package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordingAgentCommand struct {
	invocations []agentInvocation
	err         error
}

func (runner *recordingAgentCommand) Run(_ context.Context, invocation agentInvocation) error {
	runner.invocations = append(runner.invocations, invocation)
	return runner.err
}

func TestRootCommandRunsPositionalTaskWithoutReadingStdin(t *testing.T) {
	projectDirectory := t.TempDir()
	runner := &recordingAgentCommand{}
	runtime := testAgentCommandRuntime(projectDirectory, runner, false)
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	input := &countingCommandReader{reader: strings.NewReader("must not be read")}
	command.SetIn(input)
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"  Fix the failing test  "})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute positional Agent task: %v", err)
	}
	if input.reads != 0 {
		t.Fatalf("positional task consumed stdin %d time(s)", input.reads)
	}
	if len(runner.invocations) != 1 {
		t.Fatalf("unexpected invocation count: %#v", runner.invocations)
	}
	invocation := runner.invocations[0]
	if invocation.Mode != agentInvocationOnce || invocation.Task != "Fix the failing test" || invocation.Project.Path() != projectDirectory {
		t.Fatalf("unexpected positional invocation: %#v", invocation)
	}
	if invocation.Input != input || invocation.Output != stdout || invocation.ErrorOutput != stderr {
		t.Fatal("root command did not preserve configured terminal streams")
	}
}

func TestRootCommandReadsOneShotTaskFromNonTTYStdin(t *testing.T) {
	runner := &recordingAgentCommand{}
	runtime := testAgentCommandRuntime(t.TempDir(), runner, false)
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	command.SetIn(strings.NewReader("\n  inspect and fix  \n"))
	command.SetArgs(nil)
	if err := command.Execute(); err != nil {
		t.Fatalf("execute piped Agent task: %v", err)
	}
	if len(runner.invocations) != 1 || runner.invocations[0].Mode != agentInvocationOnce || runner.invocations[0].Task != "inspect and fix" {
		t.Fatalf("unexpected piped invocation: %#v", runner.invocations)
	}
}

func TestRootCommandEntersInteractiveModeForTTY(t *testing.T) {
	runner := &recordingAgentCommand{}
	runtime := testAgentCommandRuntime(t.TempDir(), runner, true)
	input := &countingCommandReader{reader: strings.NewReader("future interactive input")}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	command.SetIn(input)
	command.SetArgs(nil)
	if err := command.Execute(); err != nil {
		t.Fatalf("execute interactive Agent entry: %v", err)
	}
	if input.reads != 0 {
		t.Fatalf("interactive dispatch consumed input %d time(s)", input.reads)
	}
	if len(runner.invocations) != 1 || runner.invocations[0].Mode != agentInvocationInteractive || runner.invocations[0].Task != "" || runner.invocations[0].Input != input {
		t.Fatalf("unexpected interactive invocation: %#v", runner.invocations)
	}
}

func TestRootCommandPropagatesPlainFlag(t *testing.T) {
	runner := &recordingAgentCommand{}
	runtime := testAgentCommandRuntime(t.TempDir(), runner, true)
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	command.SetIn(&countingCommandReader{reader: strings.NewReader("unused")})
	command.SetArgs([]string{"--plain"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute plain interactive Agent entry: %v", err)
	}
	if len(runner.invocations) != 1 || !runner.invocations[0].Plain || runner.invocations[0].Mode != agentInvocationInteractive {
		t.Fatalf("plain flag was not propagated: %#v", runner.invocations)
	}
}

func TestRootCommandUsesExplicitProjectForAgent(t *testing.T) {
	startupDirectory := t.TempDir()
	explicitDirectory := filepath.Join(startupDirectory, "workspace")
	if err := os.Mkdir(explicitDirectory, 0o755); err != nil {
		t.Fatalf("create explicit project: %v", err)
	}
	runner := &recordingAgentCommand{}
	runtime := testAgentCommandRuntime(startupDirectory, runner, false)
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	command.SetArgs([]string{"--project", "workspace", "task"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute explicit-project Agent task: %v", err)
	}
	if len(runner.invocations) != 1 || runner.invocations[0].Project.Path() != explicitDirectory {
		t.Fatalf("unexpected explicit project invocation: %#v", runner.invocations)
	}
}

func TestRootCommandRejectsInvalidTaskInputs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		input    io.Reader
		terminal bool
		contains string
	}{
		{name: "empty argument", args: []string{" \n"}, input: strings.NewReader("ignored"), contains: "task argument is empty"},
		{name: "multiple arguments", args: []string{"one", "two"}, input: strings.NewReader("ignored"), contains: "accepts at most 1 arg"},
		{name: "empty pipe", input: strings.NewReader(" \n"), contains: "did not contain a task"},
		{name: "oversized pipe", input: strings.NewReader(strings.Repeat("x", maxRootTaskBytes+1)), contains: "exceeds"},
		{name: "read error", input: commandErrorReader{err: errors.New("read failed")}, contains: "read task from stdin"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingAgentCommand{}
			runtime := testAgentCommandRuntime(t.TempDir(), runner, test.terminal)
			command := newRootCommandWithRuntime(&configFlags{}, runtime)
			command.SetIn(test.input)
			command.SetArgs(test.args)
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("unexpected invalid input error: %v", err)
			}
			if len(runner.invocations) != 0 {
				t.Fatalf("invalid input invoked Agent: %#v", runner.invocations)
			}
		})
	}
}

func TestRootCommandPropagatesAgentErrorAndRequiresComposition(t *testing.T) {
	expected := errors.New("Agent failed")
	runner := &recordingAgentCommand{err: expected}
	runtime := testAgentCommandRuntime(t.TempDir(), runner, false)
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	command.SetArgs([]string{"task"})
	if err := command.Execute(); !errors.Is(err, expected) {
		t.Fatalf("unexpected Agent error: %v", err)
	}

	runtime.agentCommand = nil
	command = newRootCommandWithRuntime(&configFlags{}, runtime)
	command.SetArgs([]string{"task"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("unexpected unconfigured Agent error: %v", err)
	}
}

func TestRootManagementSubcommandsBypassAgentAndNoRunSubcommandExists(t *testing.T) {
	runner := &recordingAgentCommand{}
	runtime := testAgentCommandRuntime(t.TempDir(), runner, true)
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"version"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute management subcommand: %v", err)
	}
	if len(runner.invocations) != 0 {
		t.Fatalf("management subcommand invoked Agent: %#v", runner.invocations)
	}
	for _, subcommand := range command.Commands() {
		if subcommand.Name() == "run" {
			t.Fatal("root command unexpectedly exposes run subcommand")
		}
	}
	if command.Use != "amadeus [task]" {
		t.Fatalf("unexpected root use line: %q", command.Use)
	}
}

func TestReadRootTaskValidatesReader(t *testing.T) {
	if task, err := readRootTask(nil); err == nil || task != "" {
		t.Fatalf("unexpected nil reader result: task=%q err=%v", task, err)
	}
}

func TestParseAgentTaskSelectsExecutionMode(t *testing.T) {
	mode, task, err := parseAgentTask("你好")
	if err != nil || mode != agentExecutionReAct || task != "你好" {
		t.Fatalf("parse default ReAct task: mode=%q task=%q err=%v", mode, task, err)
	}
	mode, task, err = parseAgentTask("/plan  修改多个模块 ")
	if err != nil || mode != agentExecutionPlanned || task != "修改多个模块" {
		t.Fatalf("parse planned task: mode=%q task=%q err=%v", mode, task, err)
	}
	if _, _, err := parseAgentTask("/plan"); err == nil {
		t.Fatal("empty plan task unexpectedly parsed")
	}
}

func testAgentCommandRuntime(projectDirectory string, runner agentCommand, terminal bool) commandRuntime {
	return commandRuntime{
		amadeusRoot:      projectDirectory,
		workingDirectory: projectDirectory,
		lookupEnv:        emptyEnvLookup,
		agentCommand:     runner,
		terminalDetector: func(io.Reader) bool { return terminal },
	}
}

type countingCommandReader struct {
	reader io.Reader
	reads  int
}

func (reader *countingCommandReader) Read(content []byte) (int, error) {
	reader.reads++
	return reader.reader.Read(content)
}

type commandErrorReader struct{ err error }

func (reader commandErrorReader) Read([]byte) (int, error) { return 0, reader.err }
