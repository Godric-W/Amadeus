package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func (session *Session) startTurn(submissionID protocol.SubmissionID, input, clientUserMessageID string, kind TaskKind, modeOverride *ModeKind) (protocol.TurnID, error) {
	if input == "" {
		return "", errors.New("turn input is empty")
	}
	now := session.services.Clock().UTC()
	turnID := protocol.TurnID(session.services.NextID("turn"))
	configuration := session.Configuration()
	if modeOverride != nil {
		configuration.Mode = *modeOverride
	}
	baseContext := TurnContext{
		SubmissionID: submissionID,
		SessionID:    session.sessionID, ThreadID: session.threadID, ParentThreadID: cloneOptionalThreadID(session.parentThreadID),
		TurnID: turnID, Provider: configuration.Runtime.ModelProvider,
		Model:           configuration.Runtime.Model,
		ReasoningEffort: llm.CloneReasoningEffort(configuration.Runtime.ModelReasoningEffort),
		CWD:             configuration.CWD, Shell: configuration.Shell,
		CurrentDate: configuration.CurrentDate, Timezone: configuration.Timezone,
		Mode: configuration.Mode, Personality: configuration.Personality,
		OutputSchema:       append(json.RawMessage(nil), configuration.OutputSchema...),
		OutputSchemaStrict: configuration.OutputSchemaStrict,
	}
	createInput := threadstore.CreateInput{
		SessionID: session.sessionID, ID: session.threadID, Source: configuration.Source.Clone(), CWD: configuration.CWD, Title: titleFromInput(input),
		ModelProvider: configuration.Runtime.ModelProvider, Model: configuration.Runtime.Model, CreatedAt: now,
		BaseInstructions: session.BaseInstructions(),
	}
	materialized, err := session.materialize(session.ctx, createInput)
	if err != nil {
		session.rejectTurn(submissionID, turnID, err, false)
		return turnID, err
	}
	if materialized.MetadataWarning != nil {
		session.publish(protocol.Event{ID: submissionID, Msg: protocol.WarningEvent{ThreadID: session.threadID, TurnID: turnID, Message: materialized.MetadataWarning.Error()}})
	}
	turnState := newTurnState()
	taskValue, turnValue, err := session.createTask(session.ctx, input, baseContext, kind, turnState)
	if err != nil {
		session.rejectTurn(submissionID, turnID, err, false)
		return turnID, err
	}
	if taskValue == nil {
		err := errors.New("session task constructor returned nil task")
		session.rejectTurn(submissionID, turnID, err, false)
		return turnID, err
	}
	turnContext := &turnValue
	startedEvent := protocol.TurnStartedEvent{ThreadID: session.threadID, TurnID: turnID, StartedAt: now}
	items := make([]rollout.RolloutItem, 0, 2)
	if kind == TaskKindCompact {
		items = append(items, turnContextItem(*turnContext))
	}
	items = append(items, rollout.EventMsgItem{Msg: startedEvent})
	if err := session.appendItemsDurable(session.ctx, turnID, items...); err != nil {
		session.rejectTurn(submissionID, turnID, err, true)
		return turnID, err
	}
	if regular, ok := taskValue.(*regularTask); ok {
		regular.clientUserID = clientUserMessageID
		regular.startedAt = now
	}
	running, err := NewRunningTask(session.ctx, session, taskValue, turnContext)
	if err != nil {
		session.completeWithoutTask(submissionID, turnID, err)
		return turnID, err
	}
	session.active = &ActiveTurn{SubmissionID: submissionID, Task: running, State: turnState}
	if modeOverride != nil {
		session.applyMode(submissionID, *modeOverride)
	}
	session.publish(protocol.Event{ID: submissionID, Msg: startedEvent})
	session.watchRunningTask(running)
	return turnID, nil
}

func (session *Session) watchRunningTask(running *RunningTask) {
	if running == nil {
		return
	}
	done := running.Start()
	go func() {
		completion, ok := <-done
		if !ok {
			return
		}
		select {
		case session.completed <- completion:
		case <-session.terminated:
		}
	}()
}

func (session *Session) cancelActive(cause error) {
	if session.active != nil {
		session.active.Task.Cancel(cause)
	}
}

func (session *Session) cleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(session.ctx), 5*time.Second)
}

func titleFromInput(input string) string {
	value := []rune(strings.TrimSpace(input))
	if len(value) > 80 {
		value = value[:80]
	}
	return string(value)
}

func mustJSON(value any) json.RawMessage {
	content, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("encode canonical response item: %v", err))
	}
	return content
}
