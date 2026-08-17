package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/task"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/thread"
)

type Configuration struct {
	CWD              string
	Provider         string
	Model            string
	Shell            string
	CurrentDate      string
	Timezone         string
	PermissionMode   turn.PermissionMode
	Personality      turn.Personality
	OutputSchema     json.RawMessage
	BaseInstructions llm.BaseInstructions
}

type PermissionState struct {
	Mode turn.PermissionMode
}

type SessionState struct {
	Configuration Configuration
	History       []rollout.Line
	Permissions   PermissionState
	Context       *agentcontext.Manager
	Plan          *plan.State
}

type SessionServices struct {
	LiveThread  *thread.LiveThread
	TaskFactory task.Factory
	Clock       func() time.Time
	NextID      func(string) string
}

type SessionIo struct {
	Submissions chan<- protocol.Submission
	Events      <-chan protocol.SessionEvent
	Requests    <-chan protocol.InteractiveRequest
	Status      <-chan protocol.AgentStatus
	Terminated  <-chan struct{}
}

type SpawnArgs struct {
	ThreadID thread.ID
	History  thread.InitialHistory
	State    SessionState
	Services SessionServices
}

type ActiveTurn struct {
	State *turn.State
	Task  *task.RunningTask
}

type Session struct {
	threadID  thread.ID
	state     SessionState
	services  SessionServices
	active    *ActiveTurn
	queue     []protocol.Submission
	historyMu sync.RWMutex
	contextMu sync.Mutex

	ctx         context.Context
	cancel      context.CancelCauseFunc
	submissions chan protocol.Submission
	events      chan protocol.SessionEvent
	requests    chan protocol.InteractiveRequest
	status      chan protocol.AgentStatus
	terminated  chan struct{}
	completed   chan task.Completion
	requestsIn  chan requestDelivery
	pending     map[string]chan protocol.Op
}

type requestDelivery struct {
	request protocol.InteractiveRequest
	result  chan protocol.Op
}

var ErrInterrupted = errors.New("turn interrupted by user")

func Spawn(parent context.Context, args SpawnArgs) (*Session, SessionIo, error) {
	if parent == nil || args.ThreadID == "" || args.Services.LiveThread == nil || args.Services.TaskFactory == nil || args.Services.NextID == nil {
		return nil, SessionIo{}, errors.New("session spawn arguments are incomplete")
	}
	if args.Services.Clock == nil {
		args.Services.Clock = time.Now
	}
	if err := args.History.Validate(args.ThreadID); err != nil {
		return nil, SessionIo{}, fmt.Errorf("validate initial history: %w", err)
	}
	ctx, cancel := context.WithCancelCause(parent)
	value := &Session{
		threadID: args.ThreadID, state: args.State, services: args.Services, ctx: ctx, cancel: cancel,
		submissions: make(chan protocol.Submission, 32), events: make(chan protocol.SessionEvent, 128),
		requests: make(chan protocol.InteractiveRequest, 8), status: make(chan protocol.AgentStatus, 16),
		terminated: make(chan struct{}), completed: make(chan task.Completion, 1),
		requestsIn: make(chan requestDelivery, 8), pending: make(map[string]chan protocol.Op),
	}
	value.state.History = cloneLines(args.History.Lines)
	contextManager, err := agentcontext.NewManagerFromRollout(args.History.Lines, nil)
	if err != nil {
		cancel(err)
		return nil, SessionIo{}, fmt.Errorf("rebuild context from initial history: %w", err)
	}
	value.state.Context = contextManager
	if value.state.Plan == nil {
		value.state.Plan = plan.NewState()
	}
	if snapshot, ok := latestPlanSnapshot(args.History.Lines); ok {
		if err := value.state.Plan.Restore(snapshot); err != nil {
			cancel(err)
			return nil, SessionIo{}, fmt.Errorf("restore plan from initial history: %w", err)
		}
	}
	value.state.Permissions = PermissionState{Mode: value.state.Configuration.PermissionMode}
	io := SessionIo{
		Submissions: value.submissions, Events: value.events, Requests: value.requests,
		Status: value.status, Terminated: value.terminated,
	}
	go value.loop()
	return value, io, nil
}

func (session *Session) loop() {
	defer close(session.events)
	defer close(session.requests)
	defer close(session.status)
	defer close(session.terminated)
	defer func() {
		for requestID, result := range session.pending {
			delete(session.pending, requestID)
			result <- nil
		}
	}()
	defer func() {
		cleanupCtx, cancel := session.cleanupContext()
		defer cancel()
		_ = session.services.LiveThread.Shutdown(cleanupCtx)
	}()
	defer func() {
		if closer, ok := session.services.TaskFactory.(io.Closer); ok {
			_ = closer.Close()
		}
	}()
	session.publish(protocol.SessionEvent{ThreadID: session.threadID, Message: protocol.ThreadConfigured{}})
	sessionDone := session.ctx.Done()
	for {
		if session.active == nil && len(session.queue) > 0 {
			next := session.queue[0]
			session.queue = session.queue[1:]
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
		case completion := <-session.completed:
			session.finishTurn(completion)
			if session.ctx.Err() != nil && session.active == nil {
				return
			}
		}
	}
}

func (session *Session) handleSubmission(submission protocol.Submission) {
	switch op := submission.Op.(type) {
	case protocol.UserInputOp:
		if session.active != nil {
			session.queue = append(session.queue, submission)
			return
		}
		session.startTurn(strings.TrimSpace(op.Content), false)
	case protocol.CompactOp:
		if session.active != nil {
			session.queue = append(session.queue, submission)
			return
		}
		session.startTurn("compact context", true)
	case protocol.InterruptOp:
		session.cancelActive(ErrInterrupted)
	case protocol.ThreadSettingsOp:
		if op.PermissionMode == string(turn.PermissionModeDefault) || op.PermissionMode == string(turn.PermissionModePlan) {
			session.state.Permissions.Mode = turn.PermissionMode(op.PermissionMode)
		}
	case protocol.ApprovalDecisionOp:
		session.resolveRequest(op.RequestID, op)
	case protocol.UserInputResponseOp:
		session.resolveRequest(op.RequestID, op)
	}
}

func (session *Session) handleRequest(envelope requestDelivery) {
	if err := envelope.request.Validate(); err != nil {
		envelope.result <- nil
		return
	}
	if _, exists := session.pending[envelope.request.RequestID]; exists {
		envelope.result <- nil
		return
	}
	session.pending[envelope.request.RequestID] = envelope.result
	select {
	case session.requests <- envelope.request:
	case <-session.ctx.Done():
		delete(session.pending, envelope.request.RequestID)
		envelope.result <- nil
	}
}

func (session *Session) resolveRequest(requestID string, op protocol.Op) {
	result := session.pending[requestID]
	if result == nil {
		return
	}
	delete(session.pending, requestID)
	result <- op
}

func (session *Session) startTurn(input string, compact bool) {
	if input == "" {
		return
	}
	now := session.services.Clock().UTC()
	turnID := turn.ID(session.services.NextID("turn"))
	baseContext := turn.Context{
		ThreadID: session.threadID, TurnID: turnID, Provider: session.state.Configuration.Provider,
		Model: session.state.Configuration.Model, CWD: session.state.Configuration.CWD, Shell: session.state.Configuration.Shell,
		CurrentDate: session.state.Configuration.CurrentDate, Timezone: session.state.Configuration.Timezone,
		InitialPermissionMode: session.state.Permissions.Mode, Personality: session.state.Configuration.Personality,
		OutputSchema: append(json.RawMessage(nil), session.state.Configuration.OutputSchema...),
	}
	materialized, err := session.services.LiveThread.Materialize(session.ctx, thread.CreateInput{
		ID: session.threadID, CWD: session.state.Configuration.CWD, Title: titleFromInput(input),
		ModelProvider: session.state.Configuration.Provider, Model: session.state.Configuration.Model, CreatedAt: now,
	})
	if err != nil {
		session.rejectTurn(turnID, err, false)
		return
	}
	session.appendHistory(materialized.Lines)
	if materialized.MetadataWarning != nil {
		session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: turnID, Message: protocol.Warning{Message: materialized.MetadataWarning.Error()}})
	}
	kind := task.KindRegular
	if compact {
		kind = task.KindCompact
	}
	prepared, err := session.services.TaskFactory.Prepare(session.ctx, session, task.PrepareRequest{Kind: kind, Input: input, Context: baseContext})
	if err != nil {
		session.rejectTurn(turnID, err, false)
		return
	}
	if prepared.Task == nil {
		session.rejectTurn(turnID, errors.New("task factory returned nil task"), false)
		return
	}
	turnContext := &prepared.Context
	abortPrepared := func() {
		abortCtx, cancel := context.WithTimeout(context.WithoutCancel(session.ctx), 2*time.Second)
		defer cancel()
		_ = prepared.Task.Abort(abortCtx, session, turnContext)
	}
	contextItem, err := rollout.NewItem(rollout.KindTurnContext, turnContext)
	if err != nil {
		abortPrepared()
		session.rejectTurn(turnID, err, false)
		return
	}
	startedItem, err := rollout.NewItem(rollout.KindTurnStarted, rollout.TurnStarted{Input: input})
	if err != nil {
		abortPrepared()
		session.rejectTurn(turnID, err, false)
		return
	}
	items := []rollout.Item{contextItem}
	if !compact {
		responseItem, responseErr := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: string(llm.RoleUser), Content: input})
		if responseErr != nil {
			abortPrepared()
			session.rejectTurn(turnID, responseErr, false)
			return
		}
		items = append(items, responseItem)
	}
	items = append(items, startedItem)
	if err := session.appendItemsDurable(session.ctx, turnID, items...); err != nil {
		abortPrepared()
		session.rejectTurn(turnID, err, true)
		return
	}
	running, err := task.NewRunningTask(session.ctx, session, prepared.Task, turnContext, []task.Input{{Content: input}})
	if err != nil {
		abortPrepared()
		session.completeWithoutTask(turnID, err)
		return
	}
	session.active = &ActiveTurn{State: &turn.State{StartedAt: now}, Task: running}
	session.publishStatus(turnID, true)
	session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: turnID, Message: protocol.TurnStarted{StartedAt: now}})
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

func (session *Session) rejectTurn(turnID turn.ID, err error, fatal bool) {
	if err == nil {
		err = errors.New("turn was rejected")
	}
	session.publishStatus(turnID, false)
	session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: turnID, Message: protocol.StreamError{Error: err.Error()}})
	session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: turnID, Message: protocol.TurnRejected{Error: err.Error(), RejectedAt: session.services.Clock().UTC()}})
	if fatal {
		session.cancel(fmt.Errorf("start turn persistence: %w", err))
	}
}

func (session *Session) finishTurn(completion task.Completion) {
	if session.active == nil || session.active.Task.Context().TurnID != completion.TurnID {
		return
	}
	session.active.State.Record(completion.Result.Usage, completion.Result.ToolCallCount)
	completedItems := append([]rollout.Item(nil), completion.Result.Items...)
	if len(completedItems) > 0 {
		cleanupCtx, cancel := session.cleanupContext()
		err := session.appendItemsDurable(cleanupCtx, completion.TurnID, completedItems...)
		cancel()
		if err != nil && completion.Error == nil {
			completion.Error = err
		}
	}
	finishedAt := session.services.Clock().UTC()
	if errors.Is(completion.Cause, ErrInterrupted) || errors.Is(completion.Error, context.Canceled) && completion.Cause != nil {
		reason := completion.Cause.Error()
		summary := completion.Result.Summary
		if summary == "" {
			summary = "result: cancelled"
		}
		terminal, _ := rollout.NewItem(rollout.KindTurnAborted, rollout.TurnAborted{Summary: summary, Reason: reason})
		cleanupCtx, cancel := session.cleanupContext()
		persistErr := session.appendItemsDurable(cleanupCtx, completion.TurnID, terminal)
		cancel()
		session.active.State.MarkTerminal(persistErr)
		session.active = nil
		session.publishStatus(completion.TurnID, false)
		if persistErr != nil {
			session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: completion.TurnID, Message: protocol.StreamError{Error: persistErr.Error()}})
			session.cancel(fmt.Errorf("persist aborted turn: %w", persistErr))
			return
		}
		session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: completion.TurnID, Message: protocol.TurnAborted{Summary: summary, Reason: reason, FinishedAt: finishedAt}})
		return
	}
	status := rollout.TurnStatusCompleted
	outcome := completion.Result.Outcome
	if !outcome.Valid() {
		outcome = task.OutcomeCompleted
	}
	reason := strings.TrimSpace(completion.Result.Reason)
	errorText := ""
	if completion.Error != nil {
		status = rollout.TurnStatusFailed
		outcome = task.OutcomeFailed
		errorText = completion.Error.Error()
		if reason == "" {
			reason = errorText
		}
	} else if outcome == task.OutcomeFailed {
		status = rollout.TurnStatusFailed
		errorText = reason
		if errorText == "" {
			errorText = "task reported failed outcome"
		}
	}
	terminal, _ := rollout.NewItem(rollout.KindTurnCompleted, rollout.TurnCompleted{Status: status, Outcome: outcome, Reason: reason, Summary: completion.Result.Summary, Error: errorText})
	cleanupCtx, cancel := session.cleanupContext()
	persistErr := session.appendItemsDurable(cleanupCtx, completion.TurnID, terminal)
	cancel()
	if persistErr != nil {
		status = rollout.TurnStatusFailed
		errorText = errors.Join(completion.Error, persistErr).Error()
	}
	session.active.State.MarkTerminal(errors.Join(completion.Error, persistErr))
	session.active = nil
	session.publishStatus(completion.TurnID, false)
	if persistErr != nil {
		session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: completion.TurnID, Message: protocol.StreamError{Error: persistErr.Error()}})
		session.cancel(fmt.Errorf("persist completed turn: %w", persistErr))
		return
	}
	session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: completion.TurnID, Message: protocol.TurnCompleted{Status: status, Outcome: outcome, Reason: reason, Summary: completion.Result.Summary, Error: errorText, FinishedAt: finishedAt}})
}

func (session *Session) completeWithoutTask(turnID turn.ID, taskErr error) {
	terminal, _ := rollout.NewItem(rollout.KindTurnCompleted, rollout.TurnCompleted{Status: rollout.TurnStatusFailed, Outcome: task.OutcomeFailed, Reason: taskErr.Error(), Error: taskErr.Error()})
	cleanupCtx, cancel := session.cleanupContext()
	persistErr := session.appendItemsDurable(cleanupCtx, turnID, terminal)
	cancel()
	if persistErr != nil {
		session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: turnID, Message: protocol.StreamError{Error: persistErr.Error()}})
		session.cancel(fmt.Errorf("persist rejected turn: %w", persistErr))
		return
	}
	session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: turnID, Message: protocol.TurnCompleted{Status: rollout.TurnStatusFailed, Outcome: task.OutcomeFailed, Reason: taskErr.Error(), Error: taskErr.Error(), FinishedAt: session.services.Clock().UTC()}})
}

func (session *Session) cancelActive(cause error) {
	if session.active == nil {
		return
	}
	session.active.Task.Cancel(cause)
	abortCtx, cancel := context.WithTimeout(context.WithoutCancel(session.ctx), 2*time.Second)
	defer cancel()
	if err := session.active.Task.Abort(abortCtx); err != nil {
		session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: session.active.Task.Context().TurnID, Message: protocol.Warning{Message: err.Error()}})
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

func cloneLines(source []rollout.Line) []rollout.Line {
	lines := make([]rollout.Line, len(source))
	for index, line := range source {
		line.Item.Payload = append(json.RawMessage(nil), line.Item.Payload...)
		lines[index] = line
	}
	return lines
}
