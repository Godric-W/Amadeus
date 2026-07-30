package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

var ErrCommandTimeout = errors.New("command timed out")

type CommandExitError struct {
	ExitCode int
}

func (err *CommandExitError) Error() string {
	return fmt.Sprintf("command exited with code %d", err.ExitCode)
}

type ExecuteCommandOptions struct {
	Shell          string
	DefaultTimeout time.Duration
	MaxTimeout     time.Duration
	MaxOutputBytes int64
	MaxOutputLines int
}

type ExecuteCommand struct {
	root    project.Root
	options ExecuteCommandOptions
}

type executeCommandArguments struct {
	Command   string `json:"command"`
	CWD       string `json:"cwd,omitempty"`
	TimeoutMS int64  `json:"timeout_ms,omitempty"`
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
	if options.MaxOutputBytes <= 0 || options.MaxOutputLines <= 0 {
		return nil, errors.New("execute_command output limits must be greater than zero")
	}
	return &ExecuteCommand{root: root, options: options}, nil
}

func (executeCommand *ExecuteCommand) Spec() tool.Spec {
	return executeCommandSpec()
}

func (executeCommand *ExecuteCommand) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var arguments executeCommandArguments
	if err := decodeArguments(input, &arguments); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(arguments.Command) == "" {
		return tool.Result{}, errors.New("execute_command command is empty")
	}
	if arguments.TimeoutMS < 0 {
		return tool.Result{}, errors.New("execute_command timeout cannot be negative")
	}
	relativeCWD := arguments.CWD
	if strings.TrimSpace(relativeCWD) == "" {
		relativeCWD = "."
	}
	workingDirectory, err := executeCommand.root.Resolve(relativeCWD)
	if err != nil {
		return tool.Result{}, err
	}
	info, err := os.Stat(workingDirectory)
	if err != nil {
		return tool.Result{}, fmt.Errorf("stat execute_command cwd %q: %w", relativeCWD, err)
	}
	if !info.IsDir() {
		return tool.Result{}, fmt.Errorf("execute_command cwd is not a directory: %q", relativeCWD)
	}
	timeout := executeCommand.options.DefaultTimeout
	if arguments.TimeoutMS > 0 {
		timeout = time.Duration(arguments.TimeoutMS) * time.Millisecond
		if timeout > executeCommand.options.MaxTimeout {
			timeout = executeCommand.options.MaxTimeout
		}
	}
	if err := ctx.Err(); err != nil {
		return tool.Result{}, err
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(commandCtx, executeCommand.options.Shell, "-c", arguments.Command)
	command.Dir = workingDirectory
	configureCommandProcess(command)
	output := newBoundedOutput(executeCommand.options.MaxOutputBytes, executeCommand.options.MaxOutputLines)
	command.Stdout = output
	command.Stderr = output
	startedAt := time.Now()
	runErr := command.Run()
	duration := time.Since(startedAt)
	text, totalBytes, totalLines, truncated := output.snapshot()
	exitCode := 0
	if command.ProcessState != nil {
		exitCode = command.ProcessState.ExitCode()
	} else if runErr != nil {
		exitCode = -1
	}
	timedOut := commandCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil
	cancelled := ctx.Err() != nil
	result := tool.Result{
		ToolName: "execute_command", Text: text, Partial: truncated || timedOut || cancelled,
		Metadata: map[string]any{
			"cwd": relativeCWD, "exit_code": exitCode, "duration_ms": duration.Milliseconds(),
			"timed_out": timedOut, "cancelled": cancelled, "output_bytes": totalBytes,
			"output_lines": totalLines, "output_truncated": truncated,
		},
	}
	if timedOut {
		return result, fmt.Errorf("%w after %s", ErrCommandTimeout, timeout)
	}
	if cancelled {
		return result, ctx.Err()
	}
	if runErr != nil {
		var exitError *exec.ExitError
		if errors.As(runErr, &exitError) {
			return result, &CommandExitError{ExitCode: exitCode}
		}
		return result, fmt.Errorf("start execute_command: %w", runErr)
	}
	return result, nil
}

type boundedOutput struct {
	mutex         sync.Mutex
	buffer        bytes.Buffer
	maxBytes      int64
	maxLines      int
	totalBytes    int64
	totalNewlines int64
	lastByte      byte
	hasBytes      bool
	retainedLines int
	truncated     bool
}

func newBoundedOutput(maxBytes int64, maxLines int) *boundedOutput {
	return &boundedOutput{maxBytes: maxBytes, maxLines: maxLines}
}

func (output *boundedOutput) Write(content []byte) (int, error) {
	output.mutex.Lock()
	defer output.mutex.Unlock()
	originalLength := len(content)
	output.totalBytes += int64(originalLength)
	for _, current := range content {
		output.hasBytes = true
		output.lastByte = current
		if current == '\n' {
			output.totalNewlines++
		}
		if int64(output.buffer.Len()) >= output.maxBytes || output.retainedLines >= output.maxLines {
			output.truncated = true
			continue
		}
		_ = output.buffer.WriteByte(current)
		if current == '\n' {
			output.retainedLines++
		}
	}
	return originalLength, nil
}

func (output *boundedOutput) snapshot() (string, int64, int64, bool) {
	output.mutex.Lock()
	defer output.mutex.Unlock()
	totalLines := output.totalNewlines
	if output.hasBytes && output.lastByte != '\n' {
		totalLines++
	}
	content := output.buffer.Bytes()
	text := string(content)
	if !utf8.Valid(content) {
		text = strings.ToValidUTF8(text, "�")
	}
	return text, output.totalBytes, totalLines, output.truncated
}

var _ tool.Tool = (*ExecuteCommand)(nil)
