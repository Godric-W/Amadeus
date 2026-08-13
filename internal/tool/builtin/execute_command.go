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

	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/policy"
	processdomain "github.com/Godric-W/Amadeus/internal/process"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

var ErrCommandTimeout = errors.New("command timed out")

type CommandExitError struct{ ExitCode int }

func (err *CommandExitError) Error() string {
	return fmt.Sprintf("command exited with code %d", err.ExitCode)
}

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
	Audit            audit.Sink
}

type ExecuteCommand struct {
	root    project.Root
	policy  *project.FileSystemPolicy
	options ExecuteCommandOptions
	manager *processdomain.Manager
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
	owner      string
	displayCWD string
	yield      time.Duration
	command    processdomain.Command
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
	return &ExecuteCommand{root: root, policy: fileSystemPolicy, options: options, manager: manager, guard: policy.NewCommandGuard()}, nil
}

func (executeCommand *ExecuteCommand) Spec() tool.ToolSpec { return executeCommandSpec() }
func (executeCommand *ExecuteCommand) ProcessManager() *processdomain.Manager {
	return executeCommand.manager
}

func (executeCommand *ExecuteCommand) SupportsParallelToolCalls() bool { return false }

func (executeCommand *ExecuteCommand) Call(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	check, ok := tool.PermissionCheckFromContext(ctx)
	if !ok {
		return tool.Output{}, errors.New("execute_command must execute through ToolExecutionService")
	}
	request, ok := check.Prepared.(ExecRequest)
	if !ok {
		return tool.Output{}, errors.New("execute_command permission preparation is missing")
	}
	return executeCommand.executeExecRequest(ctx, request)
}

func (executeCommand *ExecuteCommand) CheckPermissions(ctx context.Context, invocation tool.Invocation) (tool.PermissionCheck, error) {
	var arguments executeCommandArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return tool.PermissionCheck{}, err
	}
	request, err := executeCommand.prepareExecRequest(ctx, invocation, arguments)
	if err != nil {
		return tool.PermissionCheck{}, err
	}
	command := arguments.Command
	cwd := request.command.Directory
	assessment, err := executeCommand.guard.Assess(command)
	if err != nil {
		return tool.PermissionCheck{}, fmt.Errorf("assess execute_command: %w", err)
	}
	key, ok := policy.NewCommandApprovalKey(command, cwd)
	if !ok {
		return tool.PermissionCheck{}, errors.New("execute_command approval key is invalid")
	}
	auditDecision := func(_ context.Context, decision policy.ApprovalDecision) error {
		return executeCommand.writeCommandAudit(ctx, invocation.Call, assessment.Risk, decision.Outcome, decision.Source, decision.Reason)
	}
	if assessment.Disposition == policy.CommandDeny {
		return tool.PermissionCheck{Decision: tool.PermissionDeny, Prepared: request, Reason: assessment.Reason, OnDecision: auditDecision}, nil
	}
	grant := policy.CommandGrant(key)
	approval, err := policy.NewApprovalRequestForPurpose(invocation.Call.ID, invocation.Call.Name, invocation.Call.Payload, policy.ApprovalPurposeCommand, assessment.Risk, assessment.Reason)
	if err != nil {
		return tool.PermissionCheck{}, err
	}
	approval.Command = command
	approval.CWD = cwd
	approval.Presentation = policy.CommandApprovalPresentation(command, cwd)
	return tool.PermissionCheck{Decision: tool.PermissionAsk, Request: &approval, Grant: grant, Prepared: request, OnDecision: auditDecision}, nil
}

func (executeCommand *ExecuteCommand) prepareExecRequest(ctx context.Context, invocation tool.Invocation, arguments executeCommandArguments) (ExecRequest, error) {
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
	timeout := durationFromMilliseconds(arguments.TimeoutMS, executeCommand.options.DefaultTimeout, executeCommand.options.MaxTimeout)
	yield := durationFromMilliseconds(arguments.YieldTimeMS, executeCommand.options.DefaultYield, executeCommand.options.MaxYield)
	maxTokens := arguments.MaxOutputTokens
	if maxTokens == 0 || maxTokens > executeCommand.options.MaxOutputTokens {
		maxTokens = executeCommand.options.MaxOutputTokens
	}
	maxBytes := min(int(executeCommand.options.MaxOutputBytes), maxTokens*4)
	owner := strings.TrimSpace(invocation.TurnID)
	if owner == "" {
		owner = "standalone"
	}
	return ExecRequest{
		owner: owner, displayCWD: relativeCWD, yield: yield,
		command: processdomain.Command{
			Shell: executeCommand.options.Shell, Command: arguments.Command,
			Executable: shell, Arguments: []string{"-c", arguments.Command}, Directory: resolved.Canonical,
			Timeout: timeout, TTY: arguments.TTY, MaxOutputBytes: maxBytes,
		},
	}, nil
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
	return commandSnapshotResult("execute_command", request.displayCWD, snapshot, time.Since(startedAt))
}

var _ tool.Tool = (*ExecuteCommand)(nil)

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

func commandSnapshotResult(toolName, cwd string, snapshot processdomain.Snapshot, duration time.Duration) (tool.Output, error) {
	result := tool.Output{
		ToolName: toolName, Text: snapshot.Output,
		Partial: snapshot.OutputTruncated || snapshot.State == processdomain.StateRunning || snapshot.State == processdomain.StateTimedOut || snapshot.State == processdomain.StateCancelled,
		Metadata: map[string]any{
			"process_id": string(snapshot.ID), "status": string(snapshot.State), "cwd": cwd, "exit_code": snapshot.ExitCode,
			"duration_ms": duration.Milliseconds(), "timed_out": snapshot.State == processdomain.StateTimedOut,
			"cancelled": snapshot.State == processdomain.StateCancelled, "output_bytes": snapshot.TotalOutputBytes,
			"output_truncated": snapshot.OutputTruncated,
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
		return result, &CommandExitError{ExitCode: snapshot.ExitCode}
	default:
		return result, fmt.Errorf("process has unknown state %q", snapshot.State)
	}
}

var _ tool.Tool = (*ExecuteCommand)(nil)
