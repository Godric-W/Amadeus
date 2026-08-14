package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	interfacecli "github.com/Godric-W/Amadeus/internal/interface/cli"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/render"
	"github.com/Godric-W/Amadeus/internal/rollout"
	threadmanager "github.com/Godric-W/Amadeus/internal/thread/manager"
)

func (runner *agentController) newTurnContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	factory := runner.runtime.agentContextFactory
	if factory == nil {
		factory = interruptibleTurnContext
	}
	ctx, cancel := factory(parent)
	if ctx == nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		return nil, nil, errors.New("Coding Agent turn context factory returned nil")
	}
	return ctx, cancel, nil
}

func (runner *agentController) runOnce(ctx context.Context, invocation agentInvocation) error {
	objective, err := parseAgentTask(invocation.Task)
	if err != nil {
		return err
	}
	active, _, err := runner.ensureActiveThread(ctx, invocation)
	if err != nil {
		return err
	}
	if invocation.EventSink == nil || invocation.Approvals == nil {
		renderer, approvals, interfaceErr := runner.turnInterface(invocation)
		if interfaceErr != nil {
			return interfaceErr
		}
		invocation.EventSink = renderer
		invocation.Approvals = approvals
	}
	mode := turn.PermissionModeDefault
	if invocation.RunMode == turn.PermissionModePlan {
		mode = turn.PermissionModePlan
	}
	if err := active.Submit(ctx, protocol.ThreadSettingsOp{PermissionMode: string(mode)}); err != nil {
		return err
	}
	if err := active.Submit(ctx, protocol.UserInputOp{Content: objective}); err != nil {
		return err
	}
	return runner.waitTurn(ctx, active, invocation.EventSink, invocation.Approvals)
}

func (runner *agentController) waitTurn(ctx context.Context, active *threadmanager.AmadeusThread, renderer protocol.EventSink, approvals policy.ApprovalPort) error {
	io := active.Io()
	var turnID rollout.TurnID
	interruptSent := false
	var renderErr error
	for {
		select {
		case eventValue, ok := <-io.Events:
			if !ok {
				return errors.Join(errors.New("thread terminated before turn completion"), renderErr)
			}
			if renderer != nil {
				if err := renderer.Publish(context.WithoutCancel(ctx), eventValue); err != nil {
					renderErr = errors.Join(renderErr, err)
				}
			}
			switch message := eventValue.Message.(type) {
			case protocol.TurnStarted:
				turnID = eventValue.TurnID
			case protocol.TurnRejected:
				return errors.Join(errors.New(message.Error), renderErr)
			case protocol.TurnCompleted:
				if turnID == "" || eventValue.TurnID == turnID {
					return errors.Join(commandErrorFromTerminal(message.Error), renderErr)
				}
			case protocol.TurnAborted:
				if turnID == "" || eventValue.TurnID == turnID {
					return errors.Join(&commandExitError{code: exitCodeCancelled, message: message.Reason, reported: true}, renderErr)
				}
			}
		case request, ok := <-io.Requests:
			if !ok || request.Kind != protocol.RequestApproval {
				continue
			}
			if approvals == nil {
				return errors.Join(errors.New("interactive approval port is unavailable"), renderErr)
			}
			policyRequest, err := approvalRequestFromInteractive(request)
			if err != nil {
				return errors.Join(err, renderErr)
			}
			decision, err := approvals.Decide(ctx, policyRequest)
			if err != nil {
				return errors.Join(err, renderErr)
			}
			if err := active.Submit(ctx, approvalDecisionOp(request.RequestID, decision)); err != nil {
				return errors.Join(err, renderErr)
			}
		case <-ctx.Done():
			if !interruptSent {
				interruptSent = true
				interruptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				_ = active.Submit(interruptCtx, protocol.InterruptOp{})
				cancel()
			}
		}
	}
}

func commandErrorFromTerminal(message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	code := exitCodeFailure
	if strings.HasPrefix(message, "result: partial") {
		code = exitCodePartial
	} else if strings.HasPrefix(message, "result: cancelled") {
		code = exitCodeCancelled
	}
	return &commandExitError{code: code, message: message, reported: true}
}

func (runner *agentController) turnInterface(invocation agentInvocation) (protocol.EventSink, policy.ApprovalPort, error) {
	if invocation.EventSink != nil || invocation.Approvals != nil {
		if invocation.EventSink == nil || invocation.Approvals == nil {
			return nil, nil, errors.New("Coding Agent external TUI requires both event sink and approval port")
		}
		return invocation.EventSink, invocation.Approvals, nil
	}
	detectTerminal := runner.runtime.terminalDetector
	if detectTerminal == nil {
		detectTerminal = isTerminalInput
	}
	capabilities := tui.DetectTerminalCapabilitiesWithOptions(invocation.Input, invocation.Output, tui.TerminalCapabilityOptions{IsTerminal: func(input io.Reader) bool { return detectTerminal(input) }})
	var renderer protocol.EventSink
	var approvals policy.ApprovalPort
	var err error
	if capabilities.TTY {
		renderer, err = tui.NewInlineRenderer(invocation.Output, invocation.ErrorOutput)
		if err == nil {
			approvals, err = tui.NewInlineApprovalPrompt(tui.InlineApprovalPromptOptions{Input: invocation.Input, Output: invocation.ErrorOutput, IsTerminal: func(input io.Reader) bool { return detectTerminal(input) }})
		}
	} else {
		renderer, err = render.NewAgentRenderer(invocation.Output, invocation.ErrorOutput)
		if err == nil {
			approvals, err = interfacecli.NewTerminalApprovalPrompt(interfacecli.TerminalApprovalOptions{Input: invocation.Input, Output: invocation.ErrorOutput, IsTerminal: func(input io.Reader) bool { return detectTerminal(input) }})
		}
	}
	return renderer, approvals, err
}
