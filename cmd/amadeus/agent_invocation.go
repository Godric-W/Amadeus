package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/thread"
	"github.com/spf13/cobra"
)

const maxRootTaskBytes = 1 << 20

type agentInvocationMode string

const (
	agentInvocationOnce        agentInvocationMode = "once"
	agentInvocationInteractive agentInvocationMode = "interactive"
)

type agentInvocation struct {
	Mode           agentInvocationMode
	Project        project.Root
	WorkspaceRoots []string
	Task           string
	RunMode        turn.PermissionMode
	SessionMode    sessionStartMode
	SessionID      thread.ID
	Interactive    bool
	Plain          bool
	EventSink      protocol.EventSink
	Approvals      policy.ApprovalPort
	Input          io.Reader
	Output         io.Writer
	ErrorOutput    io.Writer
}

func parseAgentTask(value string) (string, error) {
	task := strings.TrimSpace(value)
	if task == "" {
		return "", errors.New("Coding Agent task is empty")
	}
	return task, nil
}

type agentCommand interface {
	Run(context.Context, agentInvocation) error
}

type agentCommandFactory func(*cobra.Command, *configFlags, commandRuntime) (agentCommand, error)

type terminalDetector func(io.Reader) bool

func runRootAgent(command *cobra.Command, arguments []string, configFlags *configFlags, projectFlags *projectFlags, sessionFlags *sessionFlags, plain bool, runtime commandRuntime) error {
	invocation, err := resolveAgentInvocation(command, arguments, runtime)
	if err != nil {
		return err
	}
	root, err := projectFlags.resolve(command, runtime)
	if err != nil {
		return err
	}
	invocation.Project = root
	invocation.WorkspaceRoots, err = projectFlags.resolveAdditional(runtime, root)
	if err != nil {
		return err
	}
	invocation.Plain = plain
	invocation.SessionMode, invocation.SessionID, err = sessionFlags.resolve(command)
	if err != nil {
		return err
	}
	runner := runtime.agentCommand
	if runner == nil && runtime.agentCommandFactory != nil {
		runner, err = runtime.agentCommandFactory(command, configFlags, runtime)
		if err != nil {
			return err
		}
	}
	if runner == nil {
		return errors.New("Coding Agent command is not configured")
	}
	return runner.Run(command.Context(), invocation)
}

func resolveAgentInvocation(command *cobra.Command, arguments []string, runtime commandRuntime) (agentInvocation, error) {
	if command == nil {
		return agentInvocation{}, errors.New("root Agent command is nil")
	}
	if len(arguments) > 1 {
		return agentInvocation{}, fmt.Errorf("accepts at most one task argument, received %d", len(arguments))
	}
	invocation := agentInvocation{
		Input: command.InOrStdin(), Output: command.OutOrStdout(), ErrorOutput: command.ErrOrStderr(),
	}
	if len(arguments) == 1 {
		task := strings.TrimSpace(arguments[0])
		if task == "" {
			return agentInvocation{}, errors.New("task argument is empty")
		}
		invocation.Mode = agentInvocationOnce
		invocation.Task = task
		return invocation, nil
	}

	detectTerminal := runtime.terminalDetector
	if detectTerminal == nil {
		detectTerminal = isTerminalInput
	}
	if detectTerminal(invocation.Input) {
		invocation.Mode = agentInvocationInteractive
		invocation.Interactive = true
		return invocation, nil
	}
	task, err := readRootTask(invocation.Input)
	if err != nil {
		return agentInvocation{}, err
	}
	invocation.Mode = agentInvocationOnce
	invocation.Task = task
	return invocation, nil
}

func readRootTask(input io.Reader) (string, error) {
	if input == nil {
		return "", errors.New("root Agent stdin is nil")
	}
	content, err := io.ReadAll(io.LimitReader(input, maxRootTaskBytes+1))
	if err != nil {
		return "", fmt.Errorf("read task from stdin: %w", err)
	}
	if len(content) > maxRootTaskBytes {
		return "", fmt.Errorf("task from stdin exceeds %d byte limit", maxRootTaskBytes)
	}
	task := strings.TrimSpace(string(content))
	if task == "" {
		return "", errors.New("stdin is not a TTY and did not contain a task")
	}
	return task, nil
}

func isTerminalInput(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
