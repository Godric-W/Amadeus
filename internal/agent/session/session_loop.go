package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

func (session *Session) loop() {
	defer session.admissions.failAll(errors.New("session terminated before user message admission"))
	defer close(session.events)
	defer session.publish(protocol.Event{Msg: protocol.ShutdownCompleteEvent{ThreadID: session.threadID}})
	defer close(session.terminated)
	defer func() {
		session.clearPendingRequests()
	}()
	defer func() {
		cleanupCtx, cancel := session.cleanupContext()
		defer cancel()
		if session.discardOnExit.Load() {
			_ = session.services.LiveThread.Discard(cleanupCtx)
			return
		}
		_ = session.services.LiveThread.Shutdown(cleanupCtx)
	}()
	defer func() {
		cleanupCtx, cancel := session.cleanupContext()
		defer cancel()
		_ = session.services.CloseContext(cleanupCtx)
	}()
	configuredErr := session.Publish(session.ctx, protocol.Event{Msg: protocol.SessionConfiguredEvent{
		SessionID: session.sessionID, ThreadID: session.threadID, ParentThreadID: cloneOptionalThreadID(session.parentThreadID),
		Configuration: session.ProtocolConfiguration(),
	}})
	session.configured <- configuredErr
	close(session.configured)
	if configuredErr != nil {
		return
	}
	sessionDone := session.ctx.Done()
	for {
		if session.active == nil && len(session.deferred) > 0 {
			next := session.deferred[0]
			session.deferred = session.deferred[1:]
			session.handleSubmission(next)
			continue
		}
		select {
		case <-sessionDone:
			session.cancelActive(context.Cause(session.ctx))
			sessionDone = nil
			if session.active == nil {
				return
			}
		case submission, ok := <-session.submissions:
			if !ok {
				session.cancel(errors.New("session submissions closed"))
				continue
			}
			if _, shutdown := submission.Op.(protocol.ShutdownOp); shutdown {
				session.cancel(errors.New("session shutdown"))
				continue
			}
			session.handleSubmission(submission)
		case pending, ok := <-session.requestsIn:
			if !ok {
				session.cancel(errors.New("session request channel closed"))
				continue
			}
			session.handleRequest(pending)
		case request := <-session.steerRequests:
			turnID, err := session.steerInput(request.input, request.expectedTurnID)
			request.result <- steerInputResult{turnID: turnID, err: err}
		case completion := <-session.completed:
			session.finishTurn(completion)
			if session.ctx.Err() != nil && session.active == nil {
				return
			}
		}
	}
}
