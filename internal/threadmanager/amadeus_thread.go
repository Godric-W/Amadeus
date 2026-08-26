package threadmanager

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/multiagent"
	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

type AmadeusThread struct {
	manager          *ThreadManager
	sessionID        protocol.SessionID
	id               protocol.ThreadID
	parentThreadID   *protocol.ThreadID
	live             *threadstore.LiveThread
	session          *agentsession.Session
	io               agentsession.SessionIo
	nextID           func(string) string
	agentControl     *multiagent.Control
	ownsAgentControl bool
}

func (threadRuntime *AmadeusThread) ID() protocol.ThreadID {
	if threadRuntime == nil {
		return protocol.ThreadID{}
	}
	return threadRuntime.id
}

func (threadRuntime *AmadeusThread) SessionID() protocol.SessionID {
	if threadRuntime == nil {
		return protocol.SessionID{}
	}
	return threadRuntime.sessionID
}

func (threadRuntime *AmadeusThread) ParentThreadID() *protocol.ThreadID {
	if threadRuntime == nil {
		return nil
	}
	return cloneThreadID(threadRuntime.parentThreadID)
}

func cloneThreadID(id *protocol.ThreadID) *protocol.ThreadID {
	if id == nil {
		return nil
	}
	cloned := *id
	return &cloned
}

func (threadRuntime *AmadeusThread) Io() agentsession.SessionIo {
	if threadRuntime == nil {
		return agentsession.SessionIo{}
	}
	return threadRuntime.io
}

func (threadRuntime *AmadeusThread) History(ctx context.Context) ([]rollout.Line, error) {
	if threadRuntime == nil || threadRuntime.live == nil {
		return nil, errors.New("thread history is unavailable")
	}
	history, err := threadRuntime.live.History(ctx)
	if err != nil {
		return nil, err
	}
	return rollout.CloneLines(history.Lines), nil
}

func (threadRuntime *AmadeusThread) RolloutItemCount() int {
	if threadRuntime == nil || threadRuntime.session == nil {
		return 0
	}
	return threadRuntime.session.RolloutItemCount()
}

func (threadRuntime *AmadeusThread) TokenCountSnapshot() protocol.TokenCountEvent {
	if threadRuntime == nil || threadRuntime.session == nil {
		return protocol.TokenCountEvent{}
	}
	return threadRuntime.session.TokenCountSnapshot()
}

func (threadRuntime *AmadeusThread) Configuration() protocol.SessionConfiguration {
	if threadRuntime == nil || threadRuntime.session == nil {
		return protocol.SessionConfiguration{}
	}
	return threadRuntime.session.ProtocolConfiguration()
}

func (threadRuntime *AmadeusThread) ContextWindow() int64 {
	if threadRuntime == nil || threadRuntime.session == nil {
		return 0
	}
	return threadRuntime.session.Configuration().Runtime.ModelContextWindow
}

func (threadRuntime *AmadeusThread) Submit(ctx context.Context, op protocol.Op) error {
	if threadRuntime == nil || op == nil || threadRuntime.nextID == nil {
		return errors.New("thread submission is empty")
	}
	submission := protocol.Submission{ID: protocol.SubmissionID(threadRuntime.nextID("submission")), Op: op}
	select {
	case threadRuntime.io.Submissions <- submission:
		return nil
	case <-threadRuntime.io.Terminated:
		return errors.New("thread is terminated")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (threadRuntime *AmadeusThread) SubmitUserInputAndWaitForAdmission(ctx context.Context, op protocol.UserInputOp) (protocol.UserMessageAdmission, error) {
	if threadRuntime == nil || threadRuntime.nextID == nil {
		return protocol.UserMessageAdmission{}, errors.New("thread user message submission is unavailable")
	}
	if op.ClientUserMessageID == "" {
		op.ClientUserMessageID = threadRuntime.nextID("user-message")
	}
	submission := protocol.Submission{ID: protocol.SubmissionID(threadRuntime.nextID("submission")), Op: op}
	return threadRuntime.io.SubmitUserInputAndWaitForAdmission(ctx, submission)
}

func (threadRuntime *AmadeusThread) SubmitUserInput(ctx context.Context, op protocol.UserInputOp) error {
	_, err := threadRuntime.SubmitUserInputAndWaitForAdmission(ctx, op)
	return err
}

func (threadRuntime *AmadeusThread) Events() <-chan protocol.Event {
	if threadRuntime == nil {
		return nil
	}
	return threadRuntime.io.Events
}

func (threadRuntime *AmadeusThread) Terminated() <-chan struct{} {
	if threadRuntime == nil {
		return nil
	}
	return threadRuntime.io.Terminated
}

func (threadRuntime *AmadeusThread) SteerInput(ctx context.Context, expectedTurnID protocol.TurnID, content, clientUserMessageID string) (protocol.TurnID, error) {
	if threadRuntime == nil {
		return "", errors.New("thread steer input is unavailable")
	}
	if clientUserMessageID == "" && threadRuntime.nextID != nil {
		clientUserMessageID = threadRuntime.nextID("user-message")
	}
	return threadRuntime.io.SteerInput(ctx, expectedTurnID, agentsession.UserTurnInput{Content: content, ClientID: clientUserMessageID})
}

func (threadRuntime *AmadeusThread) Shutdown(ctx context.Context) error {
	if threadRuntime == nil {
		return nil
	}
	var result error
	if threadRuntime.ownsAgentControl && threadRuntime.agentControl != nil {
		result = threadRuntime.agentControl.Close(ctx)
	}
	if err := threadRuntime.Submit(ctx, protocol.ShutdownOp{}); err != nil {
		select {
		case <-threadRuntime.io.Terminated:
			return result
		default:
			return errors.Join(result, err)
		}
	}
	select {
	case <-threadRuntime.io.Terminated:
		if threadRuntime.manager != nil {
			threadRuntime.manager.removeThread(threadRuntime.id, threadRuntime)
		}
		return result
	case <-ctx.Done():
		return errors.Join(result, ctx.Err())
	}
}

func (threadRuntime *AmadeusThread) sessionRename(ctx context.Context, title string, at time.Time) error {
	if threadRuntime == nil || threadRuntime.session == nil {
		return fmt.Errorf("thread %q is unavailable", threadRuntime.id)
	}
	return threadRuntime.session.Rename(ctx, title, at)
}
