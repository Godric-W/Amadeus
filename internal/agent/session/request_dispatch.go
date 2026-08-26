package session

import "github.com/Godric-W/Amadeus/internal/protocol"

func (session *Session) handleRequest(envelope requestDelivery) {
	if err := validateRequestDelivery(envelope); err != nil {
		envelope.result <- nil
		return
	}
	if session.active == nil {
		envelope.result <- nil
		return
	}
	if _, exists := session.active.State.pendingRequests[envelope.requestID]; exists {
		envelope.result <- nil
		return
	}
	session.active.State.pendingRequests[envelope.requestID] = interactiveWaiter{kind: envelope.kind, result: envelope.result}
	if err := session.Publish(session.ctx, protocol.Event{ID: session.active.SubmissionID, Msg: protocol.ScopeEventMsg(envelope.event, session.threadID, session.active.Task.Context().TurnID)}); err != nil {
		delete(session.active.State.pendingRequests, envelope.requestID)
		envelope.result <- nil
	}
}

func (session *Session) resolveRequest(requestID protocol.RequestID, op protocol.Op) {
	if session.active == nil {
		return
	}
	waiter, ok := session.active.State.pendingRequests[requestID]
	if !ok || !interactiveResponseMatches(waiter.kind, op) {
		return
	}
	delete(session.active.State.pendingRequests, requestID)
	waiter.result <- op
}
