package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
	"github.com/spf13/cobra"
)

type agentController struct {
	command        *cobra.Command
	flags          *configFlags
	runtime        commandRuntime
	sessionRuntime *sessiondomain.SessionRuntime
	sessionCloser  io.Closer
	sessionProject string
}

func defaultAgentCommandFactory(command *cobra.Command, flags *configFlags, runtime commandRuntime) (agentCommand, error) {
	if command == nil {
		return nil, errors.New("Coding Agent Cobra command is nil")
	}
	if flags == nil {
		return nil, errors.New("Coding Agent config flags are nil")
	}
	return &agentController{command: command, flags: flags, runtime: runtime}, nil
}

func (runner *agentController) Run(ctx context.Context, invocation agentInvocation) error {
	if runner == nil || runner.command == nil || runner.flags == nil {
		return errors.New("Coding Agent command is nil")
	}
	if ctx == nil {
		return errors.New("Coding Agent context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	defer runner.closeSessionStore()
	switch invocation.Mode {
	case agentInvocationInteractive:
		return runner.runInteractive(ctx, invocation)
	case agentInvocationOnce:
		if invocation.SessionMode == sessionStartSelect {
			return errors.New("--resume without a session ID requires interactive terminal mode")
		}
		if err := runner.prepareSession(ctx, invocation, nil); err != nil {
			return err
		}
		runCtx, cancel, err := runner.newRunContext(ctx)
		if err != nil {
			return err
		}
		defer cancel()
		return runner.runOnce(runCtx, invocation)
	default:
		return fmt.Errorf("unsupported Coding Agent invocation mode %q", invocation.Mode)
	}
}

var _ agentCommand = (*agentController)(nil)
