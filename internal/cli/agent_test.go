package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/app"
	agentexec "github.com/Godric-W/Amadeus/internal/exec"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

type recordingAgentRunners struct {
	execOptions []agentexec.Options
	tuiOptions  []tui.RunOptions
	err         error
}

func (runner *recordingAgentRunners) runExec(_ context.Context, options agentexec.Options) error {
	runner.execOptions = append(runner.execOptions, options)
	return runner.err
}

func (runner *recordingAgentRunners) runTUI(_ context.Context, options tui.RunOptions) (tui.RunResult, error) {
	runner.tuiOptions = append(runner.tuiOptions, options)
	return tui.RunResult{}, runner.err
}

func TestRootCommandRunsPositionalTaskWithoutReadingStdin(t *testing.T) {
	projectDirectory := t.TempDir()
	runner := &recordingAgentRunners{}
	options := testAgentCommandOptions(t, projectDirectory, runner, false)
	command := newRootCommandWithOptions(&configFlags{}, options)
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
	if len(runner.execOptions) != 1 {
		t.Fatalf("unexpected invocation count: %#v", runner.execOptions)
	}
	invocation := runner.execOptions[0]
	if invocation.Task != "Fix the failing test" || invocation.Bootstrap.CWD != projectDirectory {
		t.Fatalf("unexpected positional invocation: %#v", invocation)
	}
	if invocation.Input != input || invocation.Output != stdout || invocation.ErrorOutput != stderr {
		t.Fatal("root command did not preserve configured terminal streams")
	}
}

func TestRootCommandReadsOneShotTaskFromNonTTYStdin(t *testing.T) {
	projectDirectory := t.TempDir()
	runner := &recordingAgentRunners{}
	command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, projectDirectory, runner, false))
	command.SetIn(strings.NewReader("\n  inspect and fix  \n"))
	command.SetArgs(nil)
	if err := command.Execute(); err != nil {
		t.Fatalf("execute piped Agent task: %v", err)
	}
	if len(runner.execOptions) != 1 || runner.execOptions[0].Task != "inspect and fix" {
		t.Fatalf("unexpected piped invocation: %#v", runner.execOptions)
	}
}

func TestRootCommandEntersInteractiveModeForTTY(t *testing.T) {
	projectDirectory := t.TempDir()
	runner := &recordingAgentRunners{}
	input := &countingCommandReader{reader: strings.NewReader("future interactive input")}
	command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, projectDirectory, runner, true))
	command.SetIn(input)
	command.SetArgs(nil)
	if err := command.Execute(); err != nil {
		t.Fatalf("execute interactive Agent entry: %v", err)
	}
	if input.reads != 0 {
		t.Fatalf("interactive dispatch consumed input %d time(s)", input.reads)
	}
	if len(runner.tuiOptions) != 1 || runner.tuiOptions[0].Input != input {
		t.Fatalf("unexpected interactive invocation: %#v", runner.tuiOptions)
	}
}

func TestRootCommandUsesExplicitProjectForAgent(t *testing.T) {
	startupDirectory := t.TempDir()
	explicitDirectory := filepath.Join(startupDirectory, "workspace")
	if err := os.Mkdir(explicitDirectory, 0o755); err != nil {
		t.Fatalf("create explicit project: %v", err)
	}
	runner := &recordingAgentRunners{}
	options := testAgentCommandOptions(t, startupDirectory, runner, false)
	command := newRootCommandWithOptions(&configFlags{}, options)
	command.SetArgs([]string{"--cd", "workspace", "task"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute explicit-project Agent task: %v", err)
	}
	if len(runner.execOptions) != 1 || runner.execOptions[0].Bootstrap.CWD != explicitDirectory {
		t.Fatalf("unexpected explicit project invocation: %#v", runner.execOptions)
	}
}

func TestRootCommandMapsSessionFlagsToTypedThreadTargets(t *testing.T) {
	projectDirectory := t.TempDir()
	threadID := testutil.ThreadID(7)
	tests := []struct {
		name string
		args []string
		want app.ThreadTarget
	}{
		{name: "new", args: []string{"task"}, want: app.ThreadTarget{Kind: app.ThreadTargetNew}},
		{name: "latest", args: []string{"--continue", "task"}, want: app.ThreadTarget{Kind: app.ThreadTargetLatest}},
		{name: "resume", args: []string{"--resume=" + threadID.String(), "task"}, want: app.ThreadTarget{Kind: app.ThreadTargetResume, ThreadID: threadID}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingAgentRunners{}
			command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, projectDirectory, runner, false))
			command.SetIn(strings.NewReader(""))
			command.SetArgs(test.args)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if len(runner.execOptions) != 1 || runner.execOptions[0].Target != test.want {
				t.Fatalf("typed target = %#v, want %#v", runner.execOptions, test.want)
			}
		})
	}
}

func TestRootCommandUsesFullscreenPickerForResumeWithoutID(t *testing.T) {
	projectDirectory := t.TempDir()
	runner := &recordingAgentRunners{}
	command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, projectDirectory, runner, true))
	command.SetIn(strings.NewReader(""))
	command.SetArgs([]string{"--resume"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(runner.tuiOptions) != 1 || !runner.tuiOptions[0].OpenSessions || runner.tuiOptions[0].Target.Kind != app.ThreadTargetNew {
		t.Fatalf("unexpected picker launch: %#v", runner.tuiOptions)
	}

	nonTTY := &recordingAgentRunners{}
	command = newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, projectDirectory, nonTTY, false))
	command.SetIn(strings.NewReader("task"))
	command.SetArgs([]string{"--resume"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "requires interactive terminal mode") {
		t.Fatalf("unexpected non-interactive selector result: %v", err)
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
			projectDirectory := t.TempDir()
			runner := &recordingAgentRunners{}
			command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, projectDirectory, runner, test.terminal))
			command.SetIn(test.input)
			command.SetArgs(test.args)
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("unexpected invalid input error: %v", err)
			}
			if len(runner.execOptions) != 0 || len(runner.tuiOptions) != 0 {
				t.Fatalf("invalid input invoked Agent: exec=%#v tui=%#v", runner.execOptions, runner.tuiOptions)
			}
		})
	}
}

func TestRootCommandPropagatesAgentError(t *testing.T) {
	expected := errors.New("Agent failed")
	projectDirectory := t.TempDir()
	runner := &recordingAgentRunners{err: expected}
	command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, projectDirectory, runner, false))
	command.SetArgs([]string{"task"})
	if err := command.Execute(); !errors.Is(err, expected) {
		t.Fatalf("unexpected Agent error: %v", err)
	}
}

func TestRootManagementSubcommandsBypassAgentAndNoRunSubcommandExists(t *testing.T) {
	projectDirectory := t.TempDir()
	runner := &recordingAgentRunners{}
	command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, projectDirectory, runner, true))
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"version"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute management subcommand: %v", err)
	}
	if len(runner.execOptions) != 0 || len(runner.tuiOptions) != 0 {
		t.Fatalf("management subcommand invoked Agent: exec=%#v tui=%#v", runner.execOptions, runner.tuiOptions)
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

func testAgentCommandOptions(t *testing.T, projectDirectory string, runner *recordingAgentRunners, terminal bool) RootOptions {
	t.Helper()
	writeCodingCommandConfig(t, projectDirectory)
	options := testAgentRootOptions(projectDirectory, projectDirectory, terminal)
	options.Runners = AgentRunners{Exec: runner.runExec, TUI: runner.runTUI}
	return options
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
