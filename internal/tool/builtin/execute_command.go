package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/policy"
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
	Shell            string
	DefaultTimeout   time.Duration
	MaxTimeout       time.Duration
	DefaultYield     time.Duration
	MaxYield         time.Duration
	MaxOutputBytes   int64
	MaxOutputLines   int
	MaxOutputTokens  int
	ProcessManager   *processdomain.Manager
	FileSystemPolicy *project.FileSystemPolicy
	Approvals        policy.ApprovalHandler
	SessionApprovals *policy.SessionApprovalStore
	Events           event.Sink
	Audit            audit.Sink
	// Sandbox and Authorizer are retained for historical callers. The
	// application bootstrap no longer supplies them.
	Sandbox    *sandboxdomain.Runner
	Authorizer *policy.CommandAuthorizer
}

type ExecuteCommand struct {
	root    project.Root
	policy  *project.FileSystemPolicy
	options ExecuteCommandOptions
	manager *processdomain.Manager
	sandbox *sandboxdomain.Runner
	guard   *policy.CommandGuard
}

type executeCommandArguments struct {
	Command         string `json:"command"`
	CWD             string `json:"cwd,omitempty"`
	TimeoutMS       int64  `json:"timeout_ms,omitempty"`
	YieldTimeMS     int64  `json:"yield_time_ms,omitempty"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
	TTY             bool   `json:"tty,omitempty"`
}

type ExecRequest struct {
	owner         string
	displayCWD    string
	yield         time.Duration
	isolationMode sandboxdomain.IsolationMode
	command       processdomain.Command
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
	fileSystemPolicy := options.FileSystemPolicy
	if fileSystemPolicy == nil {
		var err error
		fileSystemPolicy, err = project.NewFileSystemPolicy(project.FileSystemPolicyOptions{CWD: root.Path(), Profile: project.PermissionProfile{WorkspaceRoots: []string{root.Path()}}})
		if err != nil {
			return nil, err
		}
	}
	manager := options.ProcessManager
	if manager == nil {
		manager = processdomain.NewManager()
	}
	if options.SessionApprovals == nil {
		options.SessionApprovals = policy.NewSessionApprovalStore()
	}
	return &ExecuteCommand{root: root, policy: fileSystemPolicy, options: options, manager: manager, sandbox: options.Sandbox, guard: policy.NewCommandGuard()}, nil
}

func (executeCommand *ExecuteCommand) Spec() tool.Spec { return executeCommandSpec() }
func (executeCommand *ExecuteCommand) ProcessManager() *processdomain.Manager {
	return executeCommand.manager
}

func (executeCommand *ExecuteCommand) SupportsParallelToolCalls() bool { return false }

func (executeCommand *ExecuteCommand) Handle(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	call := invocation.Call
	var arguments executeCommandArguments
	if err := decodeArguments(call.Payload, &arguments); err != nil {
		return tool.Output{}, err
	}
	request, err := executeCommand.prepareExecRequest(ctx, invocation, arguments)
	if err != nil {
		return tool.Output{}, err
	}
	return executeCommand.executeExecRequest(ctx, request)
}

func (executeCommand *ExecuteCommand) prepareExecRequest(ctx context.Context, invocation tool.Invocation, arguments executeCommandArguments) (ExecRequest, error) {
	call := invocation.Call
	if strings.TrimSpace(arguments.Command) == "" {
		return ExecRequest{}, errors.New("execute_command command is empty")
	}
	if strings.ContainsRune(arguments.Command, '\x00') {
		return ExecRequest{}, errors.New("execute_command command contains NUL")
	}
	if arguments.TimeoutMS < 0 || arguments.YieldTimeMS < 0 || arguments.MaxOutputTokens < 0 {
		return ExecRequest{}, errors.New("execute_command timeout, yield and output limits cannot be negative")
	}
	relativeCWD := strings.TrimSpace(arguments.CWD)
	if relativeCWD == "" {
		relativeCWD = "."
	}
	resolved, err := executeCommand.policy.ResolveExisting(relativeCWD, project.PathDirectory)
	if err != nil {
		return ExecRequest{}, err
	}
	shell, err := exec.LookPath(executeCommand.options.Shell)
	if err != nil {
		return ExecRequest{}, fmt.Errorf("resolve execute_command shell: %w", err)
	}
	launch := sandboxdomain.Launch{Executable: shell, Arguments: []string{"-c", arguments.Command}, Directory: resolved.Canonical, Mode: sandboxdomain.IsolationUnsandboxed}
	if executeCommand.sandbox != nil {
		launch, err = executeCommand.sandbox.Prepare(shell, arguments.Command, resolved.Canonical)
		if err != nil {
			return ExecRequest{}, fmt.Errorf("prepare sandbox command: %w", err)
		}
	}
	if executeCommand.options.Authorizer != nil {
		if err := executeCommand.options.Authorizer.Authorize(ctx, policy.CommandRequest{Call: call, Shell: shell, Command: arguments.Command, CWD: resolved.Canonical, TTY: arguments.TTY, IsolationMode: launch.Mode}); err != nil {
			return ExecRequest{}, err
		}
	} else if executeCommand.sandbox == nil {
		if err := executeCommand.authorizeHostCommand(ctx, call, arguments.Command, resolved.Canonical); err != nil {
			return ExecRequest{}, err
		}
	}
	timeout := durationFromMilliseconds(arguments.TimeoutMS, executeCommand.options.DefaultTimeout, executeCommand.options.MaxTimeout)
	yield := durationFromMilliseconds(arguments.YieldTimeMS, executeCommand.options.DefaultYield, executeCommand.options.MaxYield)
	maxTokens := arguments.MaxOutputTokens
	if maxTokens == 0 || maxTokens > executeCommand.options.MaxOutputTokens {
		maxTokens = executeCommand.options.MaxOutputTokens
	}
	maxBytes := min(int(executeCommand.options.MaxOutputBytes), maxTokens*4)
	owner := strings.TrimSpace(invocation.TurnID)
	if owner == "" {
		owner = strings.TrimSpace(event.MetadataFromContext(ctx).TurnID)
	}
	if owner == "" {
		owner = "standalone"
	}
	return ExecRequest{
		owner: owner, displayCWD: relativeCWD, yield: yield, isolationMode: launch.Mode,
		command: processdomain.Command{
			Shell: executeCommand.options.Shell, Command: arguments.Command,
			Executable: launch.Executable, Arguments: launch.Arguments, Directory: launch.Directory,
			Timeout: timeout, TTY: arguments.TTY, MaxOutputBytes: maxBytes,
		},
	}, nil
}

func (executeCommand *ExecuteCommand) authorizeHostCommand(ctx context.Context, call tool.ToolCall, command, cwd string) error {
	assessment, err := executeCommand.guard.Assess(command)
	if err != nil {
		return fmt.Errorf("assess execute_command: %w", err)
	}
	if assessment.Disposition == policy.CommandDeny {
		if err := executeCommand.writeCommandAudit(ctx, call, assessment.Risk, policy.ApprovalDeny, policy.ApprovalSourcePolicy, assessment.Reason); err != nil {
			return err
		}
		return &policy.ToolDeniedError{ToolName: call.Name, Risk: assessment.Risk, Source: policy.ApprovalSourcePolicy, Reason: assessment.Reason}
	}
	key, ok := policy.NewCommandApprovalKey(command, cwd)
	if !ok {
		return errors.New("execute_command approval key is invalid")
	}
	if executeCommand.options.SessionApprovals.IsApproved(key) {
		return nil
	}
	if executeCommand.options.Approvals == nil {
		return errors.New("execute_command approval handler is nil")
	}
	request, err := policy.NewApprovalRequestForPurpose(call.ID, call.Name, call.Payload, policy.ApprovalPurposeCommand, assessment.Risk, assessment.Reason)
	if err != nil {
		return err
	}
	request.Command = command
	request.CWD = cwd
	request.Presentation = policy.CommandApprovalPresentation(command, cwd)
	if executeCommand.options.Events != nil {
		if err := executeCommand.options.Events.Publish(ctx, event.ApprovalRequested{RequestID: request.ID, ToolName: request.ToolName, Risk: string(request.Risk), Reason: request.Reason}); err != nil {
			return fmt.Errorf("publish command approval requested: %w", err)
		}
	}
	decision, err := executeCommand.options.Approvals.Decide(ctx, request)
	if err != nil {
		return fmt.Errorf("resolve command approval: %w", err)
	}
	if err := decision.Validate(); err != nil {
		return fmt.Errorf("validate command approval decision: %w", err)
	}
	if err := executeCommand.writeCommandAudit(ctx, call, assessment.Risk, decision.Outcome, decision.Source, decision.Reason); err != nil {
		return err
	}
	if executeCommand.options.Events != nil {
		if err := executeCommand.options.Events.Publish(ctx, event.ApprovalResolved{RequestID: request.ID, ToolName: request.ToolName, Outcome: string(decision.Outcome), Scope: string(decision.Scope), Source: string(decision.Source), Reason: decision.Reason}); err != nil {
			return fmt.Errorf("publish command approval resolved: %w", err)
		}
	}
	if !decision.Allowed() {
		return &policy.ToolDeniedError{ToolName: call.Name, Risk: assessment.Risk, Source: decision.Source, Reason: decision.Reason}
	}
	if decision.Scope == policy.ApprovalSession {
		executeCommand.options.SessionApprovals.Approve(key)
	}
	return nil
}

func (executeCommand *ExecuteCommand) writeCommandAudit(ctx context.Context, call tool.ToolCall, risk policy.CommandRisk, outcome policy.ApprovalOutcome, source policy.ApprovalSource, reason string) error {
	if executeCommand.options.Audit == nil {
		return nil
	}
	digest := sha256.Sum256(call.Payload)
	record := audit.Record{
		Timestamp: time.Now(), RequestID: call.ID, ToolName: call.Name,
		ArgumentsSHA256: hex.EncodeToString(digest[:]), Risk: string(risk),
		Outcome: audit.Outcome(outcome), Source: string(source), Reason: strings.TrimSpace(reason),
	}
	if err := executeCommand.options.Audit.Write(ctx, record); err != nil {
		return fmt.Errorf("write command approval audit: %w", err)
	}
	return nil
}

func (executeCommand *ExecuteCommand) executeExecRequest(ctx context.Context, request ExecRequest) (tool.Output, error) {
	startedAt := time.Now()
	processID, err := executeCommand.manager.Start(request.owner, request.command, configureCommandProcess)
	if err != nil {
		return tool.Output{}, fmt.Errorf("start execute_command: %w", err)
	}
	snapshot, err := executeCommand.manager.SnapshotContext(ctx, processID, request.owner, request.yield)
	if err != nil {
		_ = executeCommand.manager.Cancel(processID, request.owner)
		return tool.Output{}, err
	}
	return commandSnapshotResult("execute_command", request.displayCWD, snapshot, time.Since(startedAt), request.isolationMode)
}

var _ tool.Handler = (*ExecuteCommand)(nil)

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

func commandSnapshotResult(toolName, cwd string, snapshot processdomain.Snapshot, duration time.Duration, sandboxMode sandboxdomain.IsolationMode) (tool.Output, error) {
	result := tool.Output{
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

var _ tool.Handler = (*ExecuteCommand)(nil)
