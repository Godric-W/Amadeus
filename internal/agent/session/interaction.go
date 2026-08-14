package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func (session *Session) queueCompletedItem(turnID turn.ID, item rollout.Item) {
	if session == nil || turnID == "" {
		return
	}
	session.completedMu.Lock()
	session.queuedItems[turnID] = append(session.queuedItems[turnID], item)
	session.completedMu.Unlock()
}

func (session *Session) takeCompletedItems(turnID turn.ID) []rollout.Item {
	if session == nil {
		return nil
	}
	session.completedMu.Lock()
	items := append([]rollout.Item(nil), session.queuedItems[turnID]...)
	delete(session.queuedItems, turnID)
	session.completedMu.Unlock()
	return items
}

func (session *Session) publish(event protocol.SessionEvent) {
	_ = session.Publish(context.WithoutCancel(session.ctx), event)
}

// Publish is the Session-owned event boundary used by a running task. Events
// produced below the Session boundary are scoped here before they reach the
// SessionIo channel; callers do not need to carry routing metadata through
// tool or reactor internals.
func (session *Session) Publish(ctx context.Context, event protocol.SessionEvent) error {
	if session == nil {
		return errors.New("session event publisher is nil")
	}
	if ctx == nil {
		return errors.New("session event context is nil")
	}
	if event.ThreadID == "" {
		event.ThreadID = session.threadID
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if completed, ok := event.Message.(protocol.ItemCompleted); ok {
		item, err := protocol.NewCompletedItem(completed.Item)
		if err != nil {
			return fmt.Errorf("encode completed event for rollout: %w", err)
		}
		session.queueCompletedItem(turn.ID(event.TurnID), item)
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
func (session *Session) Request(ctx context.Context, request protocol.InteractiveRequest) (protocol.Op, error) {
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
			return nil, errors.New("interactive request was rejected")
		}
		return op, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-session.terminated:
		return nil, errors.New("session is terminated")
	}
}

func (session *Session) publishStatus(turnID turn.ID, working bool) {
	status := protocol.AgentStatus{ThreadID: session.threadID, TurnID: turnID, Working: working}
	select {
	case session.status <- status:
	default:
	}
}
