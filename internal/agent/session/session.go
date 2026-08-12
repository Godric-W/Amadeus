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
	ToolNames        []string
	OutputSchema     json.RawMessage
	BaseInstructions llm.BaseInstructions
}

type PermissionState struct {
	Mode turn.PermissionMode
}

type SessionState struct {
	Configuration        Configuration
	History              []rollout.Line
	PreviousTurnSettings *Configuration
	Permissions          PermissionState
	Context              *agentcontext.Manager
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

	ctx         context.Context
	cancel      context.CancelCauseFunc
	submissions chan protocol.Submission
	events      chan protocol.SessionEvent
	requests    chan protocol.InteractiveRequest
	status      chan protocol.AgentStatus
	terminated  chan struct{}
	completed   chan task.Completion
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
	}
	value.state.History = cloneLines(args.History.Lines)
	contextManager, err := agentcontext.NewManagerFromRollout(args.History.Lines, nil)
	if err != nil {
		cancel(err)
		return nil, SessionIo{}, fmt.Errorf("rebuild context from initial history: %w", err)
	}
	value.state.Context = contextManager
	value.state.PreviousTurnSettings = previousTurnSettings(args.History.Lines)
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
	}
}

func (session *Session) startTurn(input string, compact bool) {
	if input == "" {
		return
	}
	now := session.services.Clock().UTC()
	turnID := turn.ID(session.services.NextID("turn"))
	turnContext := &turn.Context{
		ThreadID: session.threadID, TurnID: turnID, Provider: session.state.Configuration.Provider,
		Model: session.state.Configuration.Model, CWD: session.state.Configuration.CWD, Shell: session.state.Configuration.Shell,
		CurrentDate: session.state.Configuration.CurrentDate, Timezone: session.state.Configuration.Timezone,
		InitialPermissionMode: session.state.Permissions.Mode, Personality: session.state.Configuration.Personality,
		ToolNames: append([]string(nil), session.state.Configuration.ToolNames...), OutputSchema: append(json.RawMessage(nil), session.state.Configuration.OutputSchema...),
	}
	if err := turnContext.Validate(); err != nil {
		session.rejectTurn(turnID, err, false)
		return
	}
	var sessionTask task.SessionTask
	var err error
	if compact {
		sessionTask, err = session.services.TaskFactory.CompactTask()
	} else {
		sessionTask, err = session.services.TaskFactory.RegularTask(input)
	}
	if err != nil {
		session.rejectTurn(turnID, err, false)
		return
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
	contextItem, err := rollout.NewItem(rollout.KindTurnContext, turnContext)
	if err != nil {
		session.rejectTurn(turnID, err, false)
		return
	}
	startedItem, err := rollout.NewItem(rollout.KindTurnStarted, rollout.TurnStarted{Input: input})
	if err != nil {
		session.rejectTurn(turnID, err, false)
		return
	}
	items := []rollout.Item{contextItem}
	if !compact {
		responseItem, responseErr := rollout.NewRawItem(rollout.KindResponseItem, mustJSON(map[string]any{"type": "user_message", "role": "user", "content": input}))
		if responseErr != nil {
			session.rejectTurn(turnID, responseErr, false)
			return
		}
		items = append(items, responseItem)
	}
	items = append(items, startedItem)
	if err := session.AppendItems(session.ctx, turnID, items...); err != nil {
		session.rejectTurn(turnID, err, true)
		return
	}
	session.state.PreviousTurnSettings = configurationFromTurnContext(*turnContext)
	running, err := task.NewRunningTask(session.ctx, session, sessionTask, turnContext, []task.Input{{Content: input}})
	if err != nil {
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

func (session *Session) AppendItems(ctx context.Context, turnID turn.ID, items ...rollout.Item) error {
	result, err := session.services.LiveThread.AppendItems(ctx, turnID, items...)
	if err != nil {
		return err
	}
	session.appendHistory(result.Lines)
	if err := session.state.Context.Rebuild(session.History()); err != nil {
		return fmt.Errorf("rebuild context after rollout append: %w", err)
	}
	if result.MetadataWarning != nil {
		session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: turnID, Message: protocol.Warning{Message: result.MetadataWarning.Error()}})
	}
	return nil
}

func (session *Session) Context() *agentcontext.Manager {
	return session.state.Context
}

func (session *Session) History() []rollout.Line {
	if session == nil {
		return nil
	}
	session.historyMu.RLock()
	defer session.historyMu.RUnlock()
	return cloneLines(session.state.History)
}

func (session *Session) appendHistory(lines []rollout.Line) {
	if len(lines) == 0 {
		return
	}
	session.historyMu.Lock()
	session.state.History = append(session.state.History, cloneLines(lines)...)
	session.historyMu.Unlock()
}

func (session *Session) Rename(ctx context.Context, title string, at time.Time) error {
	if session == nil {
		return errors.New("session is nil")
	}
	if at.IsZero() {
		return errors.New("session rename time is zero")
	}
	item, err := rollout.NewItem(rollout.KindContextUpdate, rollout.ContextUpdate{Title: strings.TrimSpace(title)})
	if err != nil {
		return err
	}
	return session.AppendItems(ctx, "", item)
}

func (session *Session) finishTurn(completion task.Completion) {
	if session.active == nil || session.active.Task.Context().TurnID != completion.TurnID {
		return
	}
	if len(completion.Result.Items) > 0 {
		cleanupCtx, cancel := session.cleanupContext()
		err := session.AppendItems(cleanupCtx, completion.TurnID, completion.Result.Items...)
		cancel()
		if err != nil && completion.Error == nil {
			completion.Error = err
		}
	}
	finishedAt := session.services.Clock().UTC()
	if errors.Is(completion.Cause, ErrInterrupted) || errors.Is(completion.Error, context.Canceled) && completion.Cause != nil {
		reason := completion.Cause.Error()
		terminal, _ := rollout.NewItem(rollout.KindTurnAborted, rollout.TurnAborted{Reason: reason})
		cleanupCtx, cancel := session.cleanupContext()
		persistErr := session.AppendItems(cleanupCtx, completion.TurnID, terminal)
		cancel()
		session.active.State.MarkTerminal(persistErr)
		session.active = nil
		session.publishStatus(completion.TurnID, false)
		if persistErr != nil {
			session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: completion.TurnID, Message: protocol.StreamError{Error: persistErr.Error()}})
			session.cancel(fmt.Errorf("persist aborted turn: %w", persistErr))
			return
		}
		session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: completion.TurnID, Message: protocol.TurnAborted{Reason: reason, FinishedAt: finishedAt}})
		return
	}
	status := rollout.TurnStatusCompleted
	errorText := ""
	if completion.Error != nil {
		status = rollout.TurnStatusFailed
		errorText = completion.Error.Error()
	}
	terminal, _ := rollout.NewItem(rollout.KindTurnCompleted, rollout.TurnCompleted{Status: status, Error: errorText})
	cleanupCtx, cancel := session.cleanupContext()
	persistErr := session.AppendItems(cleanupCtx, completion.TurnID, terminal)
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
	session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: completion.TurnID, Message: protocol.TurnCompleted{Status: status, Error: errorText, FinishedAt: finishedAt}})
}

func (session *Session) completeWithoutTask(turnID turn.ID, taskErr error) {
	terminal, _ := rollout.NewItem(rollout.KindTurnCompleted, rollout.TurnCompleted{Status: rollout.TurnStatusFailed, Error: taskErr.Error()})
	cleanupCtx, cancel := session.cleanupContext()
	persistErr := session.AppendItems(cleanupCtx, turnID, terminal)
	cancel()
	if persistErr != nil {
		session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: turnID, Message: protocol.StreamError{Error: persistErr.Error()}})
		session.cancel(fmt.Errorf("persist rejected turn: %w", persistErr))
		return
	}
	session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: turnID, Message: protocol.TurnCompleted{Status: rollout.TurnStatusFailed, Error: taskErr.Error(), FinishedAt: session.services.Clock().UTC()}})
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

func (session *Session) publish(event protocol.SessionEvent) {
	select {
	case session.events <- event:
	case <-session.ctx.Done():
	}
}

func (session *Session) publishStatus(turnID turn.ID, working bool) {
	status := protocol.AgentStatus{ThreadID: session.threadID, TurnID: turnID, Working: working}
	select {
	case session.status <- status:
	default:
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

func previousTurnSettings(lines []rollout.Line) *Configuration {
	for index := len(lines) - 1; index >= 0; index-- {
		if lines[index].Item.Kind != rollout.KindTurnContext {
			continue
		}
		var turnContext turn.Context
		if json.Unmarshal(lines[index].Item.Payload, &turnContext) != nil || turnContext.Validate() != nil {
			return nil
		}
		return configurationFromTurnContext(turnContext)
	}
	return nil
}

func configurationFromTurnContext(turnContext turn.Context) *Configuration {
	return &Configuration{
		CWD: turnContext.CWD, Provider: turnContext.Provider, Model: turnContext.Model, Shell: turnContext.Shell,
		CurrentDate: turnContext.CurrentDate, Timezone: turnContext.Timezone,
		PermissionMode: turnContext.InitialPermissionMode, Personality: turnContext.Personality,
		ToolNames: append([]string(nil), turnContext.ToolNames...), OutputSchema: append(json.RawMessage(nil), turnContext.OutputSchema...),
	}
}
