package session

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

type ThreadAutomation interface {
	StartTurnIfIdle(context.Context, TurnInput) (StartIfIdleSubmission, error)
	InjectIfRunning(context.Context, ResponseItemTurnInput) error
	PersistGoalUpdate(context.Context, protocol.ThreadGoalUpdatedEvent) error
}

type threadAutomation struct{ session *Session }

func (automation *threadAutomation) StartTurnIfIdle(ctx context.Context, input TurnInput) (StartIfIdleSubmission, error) {
	return automation.session.sessionIO().StartTurnIfIdle(ctx, input)
}

func (automation *threadAutomation) InjectIfRunning(ctx context.Context, input ResponseItemTurnInput) error {
	if err := input.validate(); err != nil {
		return err
	}
	result := make(chan error, 1)
	request := injectTurnInputRequest{input: input, result: result}
	select {
	case automation.session.injectTurnInput <- request:
	case <-automation.session.terminated:
		return errors.New("session is terminated")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-result:
		return err
	case <-automation.session.terminated:
		return errors.New("session is terminated")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (automation *threadAutomation) PersistGoalUpdate(ctx context.Context, event protocol.ThreadGoalUpdatedEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}
	configuration := automation.session.Configuration()
	create := threadstore.CreateInput{
		SessionID: automation.session.sessionID, ID: automation.session.threadID, Source: configuration.Source.Clone(),
		CWD: configuration.CWD, Title: titleFromInput(event.Goal.Objective), ModelProvider: configuration.Runtime.ModelProvider,
		Model: configuration.Runtime.Model, BaseInstructions: automation.session.BaseInstructions(), CreatedAt: automation.session.services.Clock().UTC(),
	}
	result, err := automation.session.materialize(ctx, create)
	if err != nil {
		return err
	}
	if result.MetadataWarning != nil {
		automation.session.publish(protocol.Event{Msg: protocol.WarningEvent{ThreadID: automation.session.threadID, Message: result.MetadataWarning.Error()}})
	}
	item, err := rollout.NewEventMsgItem(event)
	if err != nil {
		return err
	}
	return automation.session.appendItemsDurable(ctx, event.TurnID, item)
}

func (session *Session) sessionIO() SessionIo {
	return SessionIo{Submissions: session.submissions, Events: session.events, Terminated: session.terminated, Configured: session.configured, admissions: session.admissions, steerRequests: session.steerRequests, startIfIdleRequests: session.startIfIdleRequests}
}

type injectTurnInputRequest struct {
	input  ResponseItemTurnInput
	result chan error
}

func (session *Session) injectResponseInput(input ResponseItemTurnInput) error {
	if session.active == nil || session.active.Task == nil {
		return errors.New("no active turn")
	}
	if session.active.Task.Kind() != TaskKindRegular {
		return fmt.Errorf("active %s turn is not steerable", session.active.Task.Kind())
	}
	return session.inputQueue.Enqueue(session.active.State, input)
}

func (session *Session) publishExtensionEvent(event protocol.Event) error {
	if event.Msg == nil {
		return errors.New("extension event message is nil")
	}
	if threadID := protocol.ThreadIDOf(event.Msg); !threadID.IsZero() && threadID != session.threadID {
		return fmt.Errorf("extension event targets thread %s, expected %s", threadID, session.threadID)
	}
	if strings.TrimSpace(string(event.ID)) == "" {
		event.ID = protocol.EventID(session.services.NextID("extension-event"))
	}
	return session.Publish(context.WithoutCancel(session.ctx), event)
}

var _ ThreadAutomation = (*threadAutomation)(nil)
