package exec

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/policy"
)

const interruptWaitTimeout = 5 * time.Second

type EventProcessorOptions struct {
	Renderer  protocol.EventSink
	Approvals policy.ApprovalPort
	Input     io.Reader
	Output    io.Writer
}

type EventThread interface {
	Io() agentsession.SessionIo
	Submit(context.Context, protocol.Op) error
}

func ProcessEvents(ctx context.Context, active EventThread, options EventProcessorOptions) error {
	if ctx == nil || active == nil {
		return errors.New("one-shot event processor is incomplete")
	}
	threadIO := active.Io()
	var turnID protocol.TurnID
	var renderErr error
	userInput := newRequestUserInputPrompt(options.Input, options.Output)
	cancelled := ctx.Done()
	var interruptTimer *time.Timer
	var interruptExpired <-chan time.Time
	defer func() {
		if interruptTimer != nil {
			interruptTimer.Stop()
		}
	}()

	for {
		select {
		case eventValue, ok := <-threadIO.Events:
			if !ok {
				return errors.Join(errors.New("thread terminated before turn completion"), renderErr)
			}
			if options.Renderer != nil {
				if err := options.Renderer.Publish(context.WithoutCancel(ctx), eventValue); err != nil {
					renderErr = errors.Join(renderErr, err)
				}
			}
			switch message := eventValue.Msg.(type) {
			case protocol.TurnStartedEvent:
				turnID = message.TurnID
			case protocol.ErrorEvent:
				return errors.Join(errors.New(message.Message), renderErr)
			case protocol.TurnCompleteEvent:
				if turnID == "" || message.TurnID == turnID {
					return errors.Join(exitErrorFromTerminal(message.Error), renderErr)
				}
			case protocol.TurnAbortedEvent:
				if turnID == "" || message.TurnID == turnID {
					return errors.Join(&ExitError{Code: 130, Message: message.Reason, Reported: true}, renderErr)
				}
			case protocol.ApprovalRequestEvent:
				if options.Approvals == nil {
					return errors.Join(errors.New("one-shot approval port is unavailable"), renderErr)
				}
				request, err := approvalRequestFromEvent(message)
				if err != nil {
					return errors.Join(err, renderErr)
				}
				decision, err := options.Approvals.Decide(ctx, request)
				if err != nil {
					return errors.Join(err, renderErr)
				}
				if err := active.Submit(ctx, approvalDecisionOp(message.RequestID, decision)); err != nil {
					return errors.Join(err, renderErr)
				}
			case protocol.RequestUserInputEvent:
				response, err := userInput.Prompt(ctx, message)
				if err != nil {
					interrupt(active, ctx)
					return errors.Join(err, renderErr)
				}
				if err := active.Submit(ctx, protocol.UserInputAnswerOp{RequestID: message.RequestID, Response: response}); err != nil {
					return errors.Join(err, renderErr)
				}
			}
		case <-cancelled:
			cancelled = nil
			interrupt(active, ctx)
			interruptTimer = time.NewTimer(interruptWaitTimeout)
			interruptExpired = interruptTimer.C
		case <-interruptExpired:
			return errors.Join(&ExitError{Code: 130, Message: "turn cancellation timed out", Cause: context.Cause(ctx)}, renderErr)
		}
	}
}

func interrupt(active EventThread, parent context.Context) {
	interruptCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), 2*time.Second)
	defer cancel()
	_ = active.Submit(interruptCtx, protocol.InterruptOp{})
}

func exitErrorFromTerminal(message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	code := 1
	if strings.HasPrefix(message, "result: partial") {
		code = 2
	} else if strings.HasPrefix(message, "result: cancelled") {
		code = 130
	}
	return &ExitError{Code: code, Message: message, Reported: true}
}
