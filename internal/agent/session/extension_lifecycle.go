package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func (session *Session) runIdleLifecycle() {
	defer close(session.idleDone)
	for {
		select {
		case <-session.ctx.Done():
			return
		case cause := <-session.idleLifecycle:
			input := extension.ThreadIdleInput{Cause: cause, SessionData: session.services.sessionExtensions, ThreadData: session.services.threadExtensions}
			for _, contributor := range session.services.extensionRegistry().ThreadLifecycle() {
				if err := contributor.OnThreadIdle(session.ctx, input); err != nil && session.ctx.Err() == nil {
					session.publish(protocol.Event{Msg: protocol.WarningEvent{ThreadID: session.threadID, Message: "thread idle extension failed: " + err.Error()}})
				}
			}
		}
	}
}

func (session *Session) scheduleThreadIdle(cause extension.ThreadIdleCause) {
	select {
	case session.idleLifecycle <- cause:
	default:
	}
}

func (session *Session) EmitThreadIdle(ctx context.Context, cause extension.ThreadIdleCause) error {
	if ctx == nil {
		return errors.New("thread idle context is nil")
	}
	select {
	case session.idleLifecycle <- cause:
		return nil
	case <-session.terminated:
		return errors.New("session is terminated")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (session *Session) waitIdleLifecycle() {
	if session.idleDone != nil {
		<-session.idleDone
	}
}

func (session *Session) emitThreadStartLifecycle(ctx context.Context) error {
	configuration := session.Configuration()
	input := extension.ThreadStartInput{
		Config: configuration.Runtime, SessionSource: configuration.Source.Clone(),
		PersistentThreadStateAvailable: session.services.PersistentThreadStateAvailable,
		SessionData:                    session.services.sessionExtensions, ThreadData: session.services.threadExtensions,
	}
	for _, contributor := range session.services.extensionRegistry().ThreadLifecycle() {
		if err := contributor.OnThreadStart(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func (session *Session) EmitThreadReady(ctx context.Context) error {
	configuration := session.Configuration()
	input := extension.ThreadReadyInput{
		Config: configuration.Runtime, SessionSource: configuration.Source.Clone(),
		SessionData: session.services.sessionExtensions, ThreadData: session.services.threadExtensions,
	}
	for _, contributor := range session.services.extensionRegistry().ThreadLifecycle() {
		if err := contributor.OnThreadReady(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func (session *Session) EmitThreadResume(ctx context.Context) error {
	input := extension.ThreadResumeInput{SessionData: session.services.sessionExtensions, ThreadData: session.services.threadExtensions}
	for _, contributor := range session.services.extensionRegistry().ThreadLifecycle() {
		if err := contributor.OnThreadResume(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func (session *Session) emitThreadStopLifecycle(ctx context.Context) error {
	input := extension.ThreadStopInput{SessionData: session.services.sessionExtensions, ThreadData: session.services.threadExtensions}
	var result error
	for _, contributor := range session.services.extensionRegistry().ThreadLifecycle() {
		result = errors.Join(result, contributor.OnThreadStop(ctx, input))
	}
	return result
}

func (session *Session) emitTurnStartLifecycle(ctx context.Context, turnContext TurnContext) error {
	usage := llm.TokenUsage{}
	if snapshot := session.state.Context.TokenSnapshot(); snapshot.Info != nil {
		usage = snapshot.Info.TotalTokenUsage
	}
	input := extension.TurnStartInput{
		TurnID: turnContext.TurnID, Mode: protocol.ModeKind(turnContext.Mode), TokenUsageAtStart: usage,
		SessionData: session.services.sessionExtensions, ThreadData: session.services.threadExtensions, TurnData: turnContext.ExtensionData,
	}
	for _, contributor := range session.services.extensionRegistry().TurnLifecycle() {
		if err := contributor.OnTurnStart(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func (session *Session) emitTurnStopLifecycle(ctx context.Context, turnContext TurnContext) error {
	input := extension.TurnStopInput{SessionData: session.services.sessionExtensions, ThreadData: session.services.threadExtensions, TurnData: turnContext.ExtensionData}
	var result error
	for _, contributor := range session.services.extensionRegistry().TurnLifecycle() {
		result = errors.Join(result, contributor.OnTurnStop(ctx, input))
	}
	return result
}

func (session *Session) emitTurnAbortLifecycle(ctx context.Context, turnContext TurnContext, reason string) error {
	input := extension.TurnAbortInput{Reason: reason, SessionData: session.services.sessionExtensions, ThreadData: session.services.threadExtensions, TurnData: turnContext.ExtensionData}
	var result error
	for _, contributor := range session.services.extensionRegistry().TurnLifecycle() {
		result = errors.Join(result, contributor.OnTurnAbort(ctx, input))
	}
	return result
}

func (session *Session) emitTurnErrorLifecycle(ctx context.Context, turnContext TurnContext, cause error) error {
	input := extension.TurnErrorInput{TurnID: turnContext.TurnID, Err: cause, SessionData: session.services.sessionExtensions, ThreadData: session.services.threadExtensions, TurnData: turnContext.ExtensionData}
	var result error
	for _, contributor := range session.services.extensionRegistry().TurnLifecycle() {
		result = errors.Join(result, contributor.OnTurnError(ctx, input))
	}
	return result
}

func (session *Session) emitToolFinishLifecycle(ctx context.Context, turnContext TurnContext, executions []tool.ToolExecution) {
	for _, execution := range executions {
		input := extension.ToolFinishInput{
			TurnID: turnContext.TurnID, CallID: execution.Call.ID, ToolName: execution.Call.Name,
			Outcome: toolLifecycleOutcome(execution), SessionData: session.services.sessionExtensions,
			ThreadData: session.services.threadExtensions, TurnData: turnContext.ExtensionData,
		}
		for _, contributor := range session.services.extensionRegistry().ToolLifecycle() {
			if err := contributor.OnToolFinish(ctx, input); err != nil {
				session.publish(protocol.Event{Msg: protocol.WarningEvent{ThreadID: session.threadID, TurnID: turnContext.TurnID, Message: "tool lifecycle extension failed: " + err.Error()}})
			}
		}
	}
}

func toolLifecycleOutcome(execution tool.ToolExecution) extension.ToolCallOutcome {
	switch execution.Outcome.Status {
	case tool.ToolCallCompleted:
		return extension.ToolCallCompleted
	case tool.ToolCallDenied:
		return extension.ToolCallBlocked
	case tool.ToolCallInterrupted:
		return extension.ToolCallAborted
	case tool.ToolCallFailed:
		if execution.Outcome.Error != nil {
			switch execution.Outcome.Error.Kind {
			case "invalid_call", "not_registered", "tool_not_available", "invalid_arguments", "validation_failed", "preparation_failed", "permission_required", "permission_denied", "path_denied", "symlink_escape", "approval_denied":
				return extension.ToolCallFailedBeforeRun
			}
		}
		return extension.ToolCallFailedAfterRun
	default:
		return extension.ToolCallFailedBeforeRun
	}
}
