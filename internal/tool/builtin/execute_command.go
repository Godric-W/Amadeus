package builtin

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	processdomain "github.com/Godric-W/Amadeus/internal/process"
	"github.com/Godric-W/Amadeus/internal/project"
	sandboxdomain "github.com/Godric-W/Amadeus/internal/sandbox"
	"github.com/Godric-W/Amadeus/internal/tool"
)

var ErrCommandTimeout = errors.New("command timed out")
var ErrSandboxDenied = errors.New("sandbox denied filesystem access")

type CommandExitError struct{ ExitCode int }

func (err *CommandExitError) Error() string {
	return fmt.Sprintf("command exited with code %d", err.ExitCode)
}

type SandboxDeniedError struct {
	Output string
}

func (err *SandboxDeniedError) Error() string {
	message := strings.TrimSpace(err.Output)
	if message == "" {
		return ErrSandboxDenied.Error()
	}
	return ErrSandboxDenied.Error() + ": " + message
}

func (err *SandboxDeniedError) Unwrap() error         { return ErrSandboxDenied }
func (err *SandboxDeniedError) ToolErrorKind() string { return "sandbox_denied" }

type ExecuteCommandOptions struct {
	Shell           string
	DefaultTimeout  time.Duration
	MaxTimeout      time.Duration
	DefaultYield    time.Duration
	MaxYield        time.Duration
	MaxOutputBytes  int64
	MaxOutputLines  int
	MaxOutputTokens int
	ProcessManager  *processdomain.Manager
	PathGuard       *project.PathGuard
	Sandbox         *sandboxdomain.Runner
}

type ExecuteCommand struct {
	root    project.Root
	guard   *project.PathGuard
	options ExecuteCommandOptions
	manager *processdomain.Manager
	sandbox *sandboxdomain.Runner
}

type executeCommandArguments struct {
	Command              string                      `json:"command"`
	CWD                  string                      `json:"cwd,omitempty"`
	TimeoutMS            int64                       `json:"timeout_ms,omitempty"`
	YieldTimeMS          int64                       `json:"yield_time_ms,omitempty"`
	MaxOutputTokens      int                         `json:"max_output_tokens,omitempty"`
	TTY                  bool                        `json:"tty,omitempty"`
	RequestedPermissions requestedCommandPermissions `json:"requested_permissions,omitempty"`
}

type requestedCommandPermissions struct {
	WritableRoots []string `json:"writable_roots,omitempty"`
}

type preparedExecuteCommand struct {
	arguments executeCommandArguments
	cwd       string
	display   string
	launch    sandboxdomain.Launch
}

func NewExecuteCommand(root project.Root, options ExecuteCommandOptions) (*ExecuteCommand, error) {
	if root.Path() == "" {
		return nil, errors.New("execute_command project root is empty")
	}
	if strings.TrimSpace(options.Shell) == "" {
		options.Shell = "/bin/sh"
	}
	if options.DefaultTimeout <= 0 || options.MaxTimeout <= 0 || options.DefaultTimeout > options.MaxTimeout {
		return nil, errors.New("execute_command timeout limits are invalid")
	}
	if options.DefaultYield <= 0 {
		options.DefaultYield = 10 * time.Second
	}
	if options.MaxYield <= 0 {
		options.MaxYield = 30 * time.Second
	}
	if options.DefaultYield > options.MaxYield {
		return nil, errors.New("execute_command yield limits are invalid")
	}
	if options.MaxOutputBytes <= 0 || options.MaxOutputLines <= 0 {
		return nil, errors.New("execute_command output limits must be greater than zero")
	}
	if options.MaxOutputTokens <= 0 {
		options.MaxOutputTokens = int(options.MaxOutputBytes / 4)
	}
	guard := options.PathGuard
	if guard == nil {
		var err error
		guard, err = project.NewPathGuard(root)
		if err != nil {
			return nil, err
		}
	}
	manager := options.ProcessManager
	if manager == nil {
		manager = processdomain.NewManager()
	}
	return &ExecuteCommand{root: root, guard: guard, options: options, manager: manager, sandbox: options.Sandbox}, nil
}

func (executeCommand *ExecuteCommand) Spec() tool.Spec { return executeCommandSpec() }
func (executeCommand *ExecuteCommand) ProcessManager() *processdomain.Manager {
	return executeCommand.manager
}

func (executeCommand *ExecuteCommand) Prepare(ctx context.Context, call tool.Call) (tool.PreparedCall, error) {
	var arguments executeCommandArguments
	if err := decodeArguments(call.Arguments, &arguments); err != nil {
		return tool.PreparedCall{}, err
	}
	if strings.TrimSpace(arguments.Command) == "" {
		return tool.PreparedCall{}, errors.New("execute_command command is empty")
	}
	if strings.ContainsRune(arguments.Command, '\x00') {
		return tool.PreparedCall{}, errors.New("execute_command command contains NUL")
	}
	if arguments.TimeoutMS < 0 || arguments.YieldTimeMS < 0 || arguments.MaxOutputTokens < 0 {
		return tool.PreparedCall{}, errors.New("execute_command timeout, yield and output limits cannot be negative")
	}
	relativeCWD := strings.TrimSpace(arguments.CWD)
	if relativeCWD == "" {
		relativeCWD = "."
	}
	resolved, err := executeCommand.guard.ResolveExistingTarget(relativeCWD, project.PathDirectory)
	if err != nil {
		return tool.PreparedCall{}, err
	}
	targets := []tool.PreparedTarget{preparedFilesystemTarget(resolved)}
	for _, requestedRoot := range arguments.RequestedPermissions.WritableRoots {
		root, resolveErr := executeCommand.guard.ResolveWritableDirectoryTarget(requestedRoot)
		if resolveErr != nil {
			return tool.PreparedCall{}, resolveErr
		}
		targets = append(targets, preparedFilesystemTarget(root))
	}
	shell, err := exec.LookPath(executeCommand.options.Shell)
	if err != nil {
		return tool.PreparedCall{}, fmt.Errorf("resolve execute_command shell: %w", err)
	}
	launch := sandboxdomain.Launch{Executable: shell, Arguments: []string{"-c", arguments.Command}, Directory: resolved.Canonical, Mode: sandboxdomain.IsolationUnsandboxed}
	if executeCommand.sandbox != nil {
		launch, err = executeCommand.sandbox.Prepare(shell, arguments.Command, resolved.Canonical)
		if err != nil {
			return tool.PreparedCall{}, fmt.Errorf("prepare sandbox command: %w", err)
		}
	}
	return tool.NewPreparedCall(call, tool.PreparedOptions{
		Targets: targets, Command: arguments.Command, Shell: shell, CWD: resolved.Canonical, TTY: arguments.TTY,
		IsolationMode: string(launch.Mode), Payload: preparedExecuteCommand{arguments: arguments, cwd: resolved.Canonical, display: relativeCWD, launch: launch},
	})
}

func (executeCommand *ExecuteCommand) Execute(ctx context.Context, prepared tool.PreparedCall) (tool.Result, error) {
	payload, err := preparedPayload[preparedExecuteCommand](prepared, "execute_command")
	if err != nil {
		return tool.Result{}, err
	}
	arguments, relativeCWD, launch := payload.arguments, payload.display, payload.launch
	timeout := durationFromMilliseconds(arguments.TimeoutMS, executeCommand.options.DefaultTimeout, executeCommand.options.MaxTimeout)
	yield := durationFromMilliseconds(arguments.YieldTimeMS, executeCommand.options.DefaultYield, executeCommand.options.MaxYield)
	maxTokens := arguments.MaxOutputTokens
	if maxTokens == 0 || maxTokens > executeCommand.options.MaxOutputTokens {
		maxTokens = executeCommand.options.MaxOutputTokens
	}
	maxBytes := min(int(executeCommand.options.MaxOutputBytes), maxTokens*4)
	owner := event.MetadataFromContext(ctx).RunID
	if owner == "" {
		owner = "standalone"
	}
	startedAt := time.Now()
	processID, err := executeCommand.manager.Start(owner, processdomain.Command{
		Shell: executeCommand.options.Shell, Command: arguments.Command, Executable: launch.Executable, Arguments: launch.Arguments, Directory: launch.Directory,
		Timeout: timeout, TTY: arguments.TTY, MaxOutputBytes: maxBytes,
	}, configureCommandProcess)
	if err != nil {
		return tool.Result{}, fmt.Errorf("start execute_command: %w", err)
	}
	snapshot, err := executeCommand.manager.SnapshotContext(ctx, processID, owner, yield)
	if err != nil {
		_ = executeCommand.manager.Cancel(processID, owner)
		return tool.Result{}, err
	}
	return commandSnapshotResult("execute_command", relativeCWD, snapshot, time.Since(startedAt), launch.Mode)
}

func durationFromMilliseconds(value int64, fallback, maximum time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	result := time.Duration(value) * time.Millisecond
	if result > maximum {
		return maximum
	}
	return result
}

func commandSnapshotResult(toolName, cwd string, snapshot processdomain.Snapshot, duration time.Duration, sandboxMode sandboxdomain.IsolationMode) (tool.Result, error) {
	result := tool.Result{
		ToolName: toolName, Text: snapshot.Output,
		Partial: snapshot.OutputTruncated || snapshot.State == processdomain.StateRunning || snapshot.State == processdomain.StateTimedOut || snapshot.State == processdomain.StateCancelled,
		Metadata: map[string]any{
			"process_id": string(snapshot.ID), "status": string(snapshot.State), "cwd": cwd, "exit_code": snapshot.ExitCode,
			"duration_ms": duration.Milliseconds(), "timed_out": snapshot.State == processdomain.StateTimedOut,
			"cancelled": snapshot.State == processdomain.StateCancelled, "output_bytes": snapshot.TotalOutputBytes,
			"output_truncated": snapshot.OutputTruncated,
			"sandbox_mode":     string(sandboxMode),
		},
	}
	switch snapshot.State {
	case processdomain.StateRunning, processdomain.StateCompleted:
		return result, nil
	case processdomain.StateTimedOut:
		return result, ErrCommandTimeout
	case processdomain.StateCancelled:
		return result, context.Canceled
	case processdomain.StateFailed:
		if sandboxMode == sandboxdomain.IsolationSandboxed && sandboxFailureOutput(snapshot.Output) {
			return result, &SandboxDeniedError{Output: snapshot.Output}
		}
		return result, &CommandExitError{ExitCode: snapshot.ExitCode}
	default:
		return result, fmt.Errorf("process has unknown state %q", snapshot.State)
	}
}

func sandboxFailureOutput(output string) bool {
	value := strings.ToLower(output)
	return strings.Contains(value, "read-only file system") || strings.Contains(value, "permission denied") || strings.Contains(value, "operation not permitted")
}

var _ tool.Tool = (*ExecuteCommand)(nil)
