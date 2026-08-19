package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

func (session *Session) clearPendingRequests() {
	if session == nil || session.active == nil {
		return
	}
	for requestID, result := range session.active.pending {
		delete(session.active.pending, requestID)
		result <- nil
	}
}

func (session *Session) publish(event protocol.Event) {
	_ = session.Publish(context.WithoutCancel(session.ctx), event)
}

// Publish is the Session-owned event boundary used by a running task. Events
// produced below the Session boundary are scoped here before they reach the
// SessionIo channel; callers do not need to carry routing metadata through
// tool or engine internals.
func (session *Session) Publish(ctx context.Context, event protocol.Event) error {
	if session == nil {
		return errors.New("session event publisher is nil")
	}
	if ctx == nil {
		return errors.New("session event context is nil")
	}
	if event.ID == "" {
		event.ID = protocol.SubmissionID(session.services.NextID("event"))
	}
	event.Msg = protocol.ScopeEventMsg(event.Msg, protocol.ThreadID(session.threadID), protocol.TurnIDOf(event.Msg))
	if err := event.Validate(); err != nil {
		return err
	}
	select {
	case session.events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-session.terminated:
		return errors.New("session event channel is closed")
	}
}

// Request is the Session-owned request/response boundary for approvals and
// other interactive tool interactions. The Session loop remains the sole
// owner of pending requests and routes the response back to the caller.
func (session *Session) Request(ctx context.Context, request protocol.ApprovalRequestEvent) (protocol.Op, error) {
	if session == nil {
		return nil, errors.New("session request publisher is nil")
	}
	if ctx == nil {
		return nil, errors.New("session request context is nil")
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	result := make(chan protocol.Op, 1)
	envelope := requestDelivery{request: request, result: result}
	select {
	case session.requestsIn <- envelope:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-session.terminated:
		return nil, errors.New("session is terminated")
	}
	select {
	case op := <-result:
		if op == nil {
			return nil, errors.New("approval request was rejected")
		}
		return op, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-session.terminated:
		return nil, errors.New("session is terminated")
	}
}
