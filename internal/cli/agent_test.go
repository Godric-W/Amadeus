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
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/bootstrap"
	"github.com/Godric-W/Amadeus/internal/testutil"
	"github.com/Godric-W/Amadeus/internal/tui"
)

type recordingTUIRunner struct {
	options []tui.RunOptions
	err     error
}

func (runner *recordingTUIRunner) Run(_ context.Context, options tui.RunOptions) (tui.RunResult, error) {
	runner.options = append(runner.options, options)
	return tui.RunResult{}, runner.err
}

func TestRootCommandRoutesPositionalPromptToTUIWithoutReadingStdin(t *testing.T) {
	projectDirectory := t.TempDir()
	runner := &recordingTUIRunner{}
	command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, projectDirectory, runner, true))
	input := &countingCommandReader{reader: strings.NewReader("must not be read")}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command.SetIn(input)
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"  Fix\r\nthe test  "})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute positional prompt: %v", err)
	}
	if input.reads != 0 {
		t.Fatalf("positional prompt consumed stdin %d time(s)", input.reads)
	}
	if len(runner.options) != 1 {
		t.Fatalf("TUI invocations = %#v", runner.options)
	}
	invocation := runner.options[0]
	if invocation.Prompt != "  Fix\nthe test  " || invocation.Bootstrap.CWD != projectDirectory {
		t.Fatalf("unexpected TUI invocation: %#v", invocation)
	}
	if invocation.Input != input || invocation.Output != stdout || invocation.ErrorOutput != stderr {
		t.Fatal("root command did not preserve configured terminal streams")
	}
}

func TestRootCommandWithoutPromptStillRoutesToTUI(t *testing.T) {
	projectDirectory := t.TempDir()
	runner := &recordingTUIRunner{}
	command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, projectDirectory, runner, false))
	input := &countingCommandReader{reader: strings.NewReader("piped input must not become a prompt")}
	command.SetIn(input)
	command.SetArgs(nil)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if input.reads != 0 || len(runner.options) != 1 || runner.options[0].Prompt != "" {
		t.Fatalf("unexpected promptless dispatch: reads=%d options=%#v", input.reads, runner.options)
	}
}

func TestRootCommandUsesExplicitProjectForTUI(t *testing.T) {
	startupDirectory := t.TempDir()
	explicitDirectory := filepath.Join(startupDirectory, "workspace")
	if err := os.Mkdir(explicitDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &recordingTUIRunner{}
	command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, startupDirectory, runner, true))
	command.SetArgs([]string{"--cd", "workspace", "prompt"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(runner.options) != 1 || runner.options[0].Bootstrap.CWD != explicitDirectory {
		t.Fatalf("unexpected explicit project launch: %#v", runner.options)
	}
}

func TestRootCommandMapsSessionFlagsAndPromptToTUI(t *testing.T) {
	projectDirectory := t.TempDir()
	threadID := testutil.ThreadID(7)
	tests := []struct {
		name string
		args []string
		want app.ThreadTarget
	}{
		{name: "new", args: []string{"prompt"}, want: app.ThreadTarget{Kind: app.ThreadTargetNew}},
		{name: "latest", args: []string{"--continue", "prompt"}, want: app.ThreadTarget{Kind: app.ThreadTargetLatest}},
		{name: "resume", args: []string{"--resume=" + threadID.String(), "prompt"}, want: app.ThreadTarget{Kind: app.ThreadTargetResume, ThreadID: threadID}},
		{name: "resume separate ID and prompt", args: []string{"--resume", threadID.String(), "prompt"}, want: app.ThreadTarget{Kind: app.ThreadTargetResume, ThreadID: threadID}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingTUIRunner{}
			command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, projectDirectory, runner, true))
			command.SetArgs(test.args)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if len(runner.options) != 1 || runner.options[0].Target != test.want || runner.options[0].Prompt != "prompt" {
				t.Fatalf("TUI options = %#v, want target %#v", runner.options, test.want)
			}
		})
	}
}

func TestRootCommandUsesTUIPickerOnlyWithoutPrompt(t *testing.T) {
	projectDirectory := t.TempDir()
	runner := &recordingTUIRunner{}
	command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, projectDirectory, runner, true))
	command.SetArgs([]string{"--resume"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(runner.options) != 1 || !runner.options[0].OpenSessions || runner.options[0].Prompt != "" || runner.options[0].Target.Kind != app.ThreadTargetNew {
		t.Fatalf("unexpected picker launch: %#v", runner.options)
	}
}

func TestRootCommandRejectsInvalidPromptArguments(t *testing.T) {
	projectDirectory := t.TempDir()
	for _, test := range []struct {
		name     string
		args     []string
		contains string
	}{
		{name: "multiple", args: []string{"one", "two"}, contains: "accepts at most 1 arg"},
		{name: "oversized", args: []string{strings.Repeat("x", maxInitialPromptBytes+1)}, contains: "prompt argument exceeds"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingTUIRunner{}
			command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, projectDirectory, runner, true))
			command.SetArgs(test.args)
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), test.contains) || len(runner.options) != 0 {
				t.Fatalf("invalid prompt result: err=%v options=%#v", err, runner.options)
			}
		})
	}
}

func TestRootCommandTreatsExactEmptyPromptAsNoInitialMessage(t *testing.T) {
	runner := &recordingTUIRunner{}
	command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, t.TempDir(), runner, true))
	command.SetArgs([]string{""})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(runner.options) != 1 || runner.options[0].Prompt != "" {
		t.Fatalf("empty prompt dispatch = %#v", runner.options)
	}
}

func TestRootCommandPropagatesTUIError(t *testing.T) {
	expected := errors.New("TUI failed")
	runner := &recordingTUIRunner{err: expected}
	command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, t.TempDir(), runner, true))
	command.SetArgs([]string{"prompt"})
	if err := command.Execute(); !errors.Is(err, expected) {
		t.Fatalf("unexpected TUI error: %v", err)
	}
}

func TestRootManagementSubcommandsBypassTUI(t *testing.T) {
	runner := &recordingTUIRunner{}
	command := newRootCommandWithOptions(&configFlags{}, testAgentCommandOptions(t, t.TempDir(), runner, true))
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"version"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(runner.options) != 0 {
		t.Fatalf("management subcommand invoked TUI: %#v", runner.options)
	}
	if command.Use != "amadeus [PROMPT]" {
		t.Fatalf("root use = %q", command.Use)
	}
}

func testAgentCommandOptions(t *testing.T, projectDirectory string, runner *recordingTUIRunner, terminal bool) RootOptions {
	t.Helper()
	writeTestAgentConfig(t, projectDirectory)
	environment := bootstrap.Environment{AmadeusRoot: projectDirectory, WorkingDirectory: projectDirectory, LookupEnv: emptyEnvLookup}
	dependencies := bootstrap.DefaultDependencies(environment)
	dependencies.AuditFactory = func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil }
	return RootOptions{
		Environment: environment, Bootstrap: dependencies, TUI: runner.Run,
		IsTerminal: func(io.Reader) bool { return terminal },
	}
}

func writeTestAgentConfig(t *testing.T, directory string) {
	t.Helper()
	content := `model: mock-model
model_provider: mock
model_context_window: 8192
model_providers:
  mock:
    wire_api: responses
    dialect: standard
    api_key: test
    base_url: https://example.invalid/v1
`
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
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
