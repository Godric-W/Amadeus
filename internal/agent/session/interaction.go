package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type requestDelivery struct {
	kind      interactiveRequestKind
	requestID protocol.RequestID
	event     protocol.EventMsg
	result    chan protocol.Op
}

type interactiveRequestKind string

const (
	interactiveApproval  interactiveRequestKind = "approval"
	interactiveUserInput interactiveRequestKind = "request_user_input"
)

type interactiveWaiter struct {
	kind   interactiveRequestKind
	result chan protocol.Op
}

func (session *Session) clearPendingRequests() {
	if session == nil || session.active == nil {
		return
	}
	for requestID, waiter := range session.active.State.pendingRequests {
		delete(session.active.State.pendingRequests, requestID)
		waiter.result <- nil
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
	envelope := requestDelivery{kind: interactiveApproval, requestID: request.RequestID, event: request, result: result}
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

func (session *Session) RequestUserInput(ctx context.Context, callID string, args tool.RequestUserInputArgs) (tool.RequestUserInputResponse, error) {
	if session == nil {
		return tool.RequestUserInputResponse{}, errors.New("session request publisher is nil")
	}
	if ctx == nil {
		return tool.RequestUserInputResponse{}, errors.New("session request context is nil")
	}
	if err := args.Validate(); err != nil {
		return tool.RequestUserInputResponse{}, err
	}
	requestID := protocol.RequestID(session.services.NextID("request"))
	request := protocol.RequestUserInputEvent{RequestID: requestID, CallID: callID, RequestUserInputArgs: args}
	result := make(chan protocol.Op, 1)
	envelope := requestDelivery{kind: interactiveUserInput, requestID: requestID, event: request, result: result}
	select {
	case session.requestsIn <- envelope:
	case <-ctx.Done():
		return tool.RequestUserInputResponse{}, ctx.Err()
	case <-session.terminated:
		return tool.RequestUserInputResponse{}, errors.New("session is terminated")
	}
	select {
	case op := <-result:
		answer, ok := op.(protocol.UserInputAnswerOp)
		if !ok {
			return tool.RequestUserInputResponse{}, errors.New("request_user_input was cancelled before receiving a response")
		}
		if err := answer.Response.Validate(args); err != nil {
			return tool.RequestUserInputResponse{}, err
		}
		return answer.Response, nil
	case <-ctx.Done():
		return tool.RequestUserInputResponse{}, ctx.Err()
	case <-session.terminated:
		return tool.RequestUserInputResponse{}, errors.New("session is terminated")
	}
}

func validateRequestDelivery(envelope requestDelivery) error {
	if envelope.result == nil || envelope.requestID == "" || envelope.event == nil {
		return errors.New("interactive request is incomplete")
	}
	switch event := envelope.event.(type) {
	case protocol.ApprovalRequestEvent:
		if envelope.kind != interactiveApproval || event.RequestID != envelope.requestID {
			return errors.New("approval request envelope is inconsistent")
		}
		return event.Validate()
	case protocol.RequestUserInputEvent:
		if envelope.kind != interactiveUserInput || event.RequestID != envelope.requestID {
			return errors.New("request_user_input envelope is inconsistent")
		}
		return event.Validate()
	default:
		return errors.New("interactive request type is unsupported")
	}
}

func interactiveResponseMatches(kind interactiveRequestKind, op protocol.Op) bool {
	switch kind {
	case interactiveApproval:
		_, ok := op.(protocol.ApprovalDecisionOp)
		return ok
	case interactiveUserInput:
		_, ok := op.(protocol.UserInputAnswerOp)
		return ok
	default:
		return false
	}
}
