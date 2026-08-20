package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/thread"
)

type Configuration struct {
	CWD                string
	Provider           string
	Model              string
	Shell              string
	CurrentDate        string
	Timezone           string
	Mode               turn.ModeKind
	Personality        turn.Personality
	OutputSchema       json.RawMessage
	OutputSchemaStrict bool
}

type ModeState struct {
	Mode turn.ModeKind
}

type SessionState struct {
	Configuration Configuration
	Mode          ModeState
	Context       *agentcontext.Manager
	Plan          *plan.State
}

type SessionServices struct {
	LiveThread       *thread.LiveThread
	TaskConstructors TaskConstructors
	AgentServices    *engine.Services
	Clock            func() time.Time
	NextID           func(string) string
}

type SessionIo struct {
	Submissions chan<- protocol.Submission
	Events      <-chan protocol.Event
	Terminated  <-chan struct{}
}

type SpawnArgs struct {
	ThreadID      protocol.ThreadID
	History       thread.InitialHistory
	State         SessionState
	Services      SessionServices
	BuildServices func(context.Context, *Session) (*engine.Services, error)
}

type ActiveTurn struct {
	SubmissionID protocol.SubmissionID
	State        *turn.TurnState
	Task         *RunningTask
	pending      map[protocol.RequestID]chan protocol.Op
}

type Session struct {
	threadID protocol.ThreadID
	state    SessionState
	services SessionServices
	active   *ActiveTurn
	queue    []protocol.Submission
	appendMu sync.Mutex
	modeMu   sync.RWMutex

	ctx         context.Context
	cancel      context.CancelCauseFunc
	submissions chan protocol.Submission
	events      chan protocol.Event
	terminated  chan struct{}
	completed   chan Completion
	requestsIn  chan requestDelivery
}

const compactionWarningMessage = "Heads up: Long threads and multiple compactions can cause the model to be less accurate. Start a new thread when possible to keep threads small and targeted."

type requestDelivery struct {
	request protocol.ApprovalRequestEvent
	result  chan protocol.Op
}

var ErrInterrupted = errors.New("turn interrupted by user")

func Spawn(parent context.Context, args SpawnArgs) (*Session, SessionIo, error) {
	if parent == nil || args.ThreadID == "" || args.Services.LiveThread == nil || !args.Services.TaskConstructors.Valid() || args.Services.NextID == nil {
		return nil, SessionIo{}, errors.New("session spawn arguments are incomplete")
	}
	if args.Services.Clock == nil {
		args.Services.Clock = time.Now
	}
	if err := args.History.Validate(args.ThreadID); err != nil {
		return nil, SessionIo{}, fmt.Errorf("validate initial history: %w", err)
	}
	ctx, cancel := context.WithCancelCause(parent)
	closeSpawnServices := func() {
		if args.Services.TaskConstructors.Close != nil {
			_ = args.Services.TaskConstructors.Close()
		}
		_ = args.Services.LiveThread.Shutdown(context.Background())
	}
	value := &Session{
		threadID: args.ThreadID, state: args.State, services: args.Services, ctx: ctx, cancel: cancel,
		submissions: make(chan protocol.Submission, 32), events: make(chan protocol.Event, 128),
		terminated: make(chan struct{}), completed: make(chan Completion, 1),
		requestsIn: make(chan requestDelivery, 8),
	}
	contextManager, err := agentcontext.NewManagerFromRollout(args.History.Lines, nil)
	if err != nil {
		cancel(err)
		closeSpawnServices()
		return nil, SessionIo{}, fmt.Errorf("rebuild context from initial history: %w", err)
	}
	value.state.Context = contextManager
	if value.state.Plan == nil {
		value.state.Plan = plan.NewState()
	}
	if snapshot, ok := latestPlanSnapshot(args.History.Lines); ok {
		if err := value.state.Plan.Restore(snapshot); err != nil {
			cancel(err)
			closeSpawnServices()
			return nil, SessionIo{}, fmt.Errorf("restore plan from initial history: %w", err)
		}
	}
	value.state.Mode = ModeState{Mode: value.state.Configuration.Mode}
	if args.BuildServices != nil {
		capabilities, buildErr := args.BuildServices(ctx, value)
		if buildErr != nil {
			cancel(buildErr)
			closeSpawnServices()
			return nil, SessionIo{}, fmt.Errorf("build session services: %w", buildErr)
		}
		value.services.AgentServices = capabilities
	}
	io := SessionIo{
		Submissions: value.submissions, Events: value.events, Terminated: value.terminated,
	}
	go value.loop()
	return value, io, nil
}

func (session *Session) loop() {
	defer close(session.events)
	defer session.publish(protocol.Event{Msg: protocol.ShutdownCompleteEvent{ThreadID: protocol.ThreadID(session.threadID)}})
	defer close(session.terminated)
	defer func() {
		session.clearPendingRequests()
	}()
	defer func() {
		cleanupCtx, cancel := session.cleanupContext()
		defer cancel()
		_ = session.services.LiveThread.Shutdown(cleanupCtx)
	}()
	defer func() {
		if session.services.AgentServices != nil {
			_ = session.services.AgentServices.Close()
		}
	}()
	defer func() {
		if session.services.TaskConstructors.Close != nil {
			_ = session.services.TaskConstructors.Close()
		}
	}()
	session.publish(protocol.Event{Msg: protocol.SessionConfiguredEvent{
		ThreadID: protocol.ThreadID(session.threadID),
		Configuration: protocol.SessionConfiguration{
			CWD: session.state.Configuration.CWD, Provider: session.state.Configuration.Provider,
			Model: session.state.Configuration.Model, Mode: string(session.state.Configuration.Mode),
		},
	}})
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
	if err := submission.Validate(); err != nil {
		session.publish(protocol.Event{ID: submission.ID, Msg: protocol.ErrorEvent{ThreadID: protocol.ThreadID(session.threadID), Code: "invalid_submission", Message: err.Error(), At: session.services.Clock().UTC()}})
		return
	}
	switch op := submission.Op.(type) {
	case protocol.UserInputOp:
		if session.active != nil {
			session.queue = append(session.queue, submission)
			return
		}
		session.startTurn(submission.ID, strings.TrimSpace(op.Content), false)
	case protocol.CompactOp:
		if session.active != nil {
			session.queue = append(session.queue, submission)
			return
		}
		session.startTurn(submission.ID, "compact context", true)
	case protocol.InterruptOp:
		session.cancelActive(ErrInterrupted)
	case protocol.ThreadSettingsOp:
		if op.Mode == string(turn.ModeKindDefault) || op.Mode == string(turn.ModeKindPlan) {
			session.modeMu.Lock()
			session.state.Mode.Mode = turn.ModeKind(op.Mode)
			session.modeMu.Unlock()
			session.publish(protocol.Event{ID: submission.ID, Msg: protocol.ThreadSettingsAppliedEvent{ThreadID: protocol.ThreadID(session.threadID), Mode: op.Mode}})
		}
	case protocol.ApprovalDecisionOp:
		session.resolveRequest(op.RequestID, op)
	}
}

func (session *Session) handleRequest(envelope requestDelivery) {
	if err := envelope.request.Validate(); err != nil {
		envelope.result <- nil
		return
	}
	if session.active == nil {
		envelope.result <- nil
		return
	}
	if _, exists := session.active.pending[envelope.request.RequestID]; exists {
		envelope.result <- nil
		return
	}
	session.active.pending[envelope.request.RequestID] = envelope.result
	session.publish(protocol.Event{ID: session.active.SubmissionID, Msg: protocol.ScopeEventMsg(envelope.request, protocol.ThreadID(session.threadID), protocol.TurnID(session.active.Task.Context().TurnID))})
}

func (session *Session) resolveRequest(requestID protocol.RequestID, op protocol.Op) {
	if session.active == nil {
		return
	}
	result := session.active.pending[requestID]
	if result == nil {
		return
	}
	delete(session.active.pending, requestID)
	result <- op
}

func (session *Session) startTurn(submissionID protocol.SubmissionID, input string, compact bool) {
	if input == "" {
		return
	}
	now := session.services.Clock().UTC()
	turnID := protocol.TurnID(session.services.NextID("turn"))
	baseContext := turn.TurnContext{
		SubmissionID: submissionID,
		ThreadID:     session.threadID, TurnID: turnID, Provider: session.state.Configuration.Provider,
		Model: session.state.Configuration.Model, CWD: session.state.Configuration.CWD, Shell: session.state.Configuration.Shell,
		CurrentDate: session.state.Configuration.CurrentDate, Timezone: session.state.Configuration.Timezone,
		Mode: session.Mode(), Personality: session.state.Configuration.Personality,
		OutputSchema:       append(json.RawMessage(nil), session.state.Configuration.OutputSchema...),
		OutputSchemaStrict: session.state.Configuration.OutputSchemaStrict,
	}
	createInput := thread.CreateInput{
		ID: session.threadID, CWD: session.state.Configuration.CWD, Title: titleFromInput(input),
		ModelProvider: session.state.Configuration.Provider, Model: session.state.Configuration.Model, CreatedAt: now,
	}
	materialized, err := session.materialize(session.ctx, createInput)
	if err != nil {
		session.rejectTurn(submissionID, turnID, err, false)
		return
	}
	if materialized.MetadataWarning != nil {
		session.publish(protocol.Event{ID: submissionID, Msg: protocol.WarningEvent{ThreadID: protocol.ThreadID(session.threadID), TurnID: protocol.TurnID(turnID), Message: materialized.MetadataWarning.Error()}})
	}
	constructor := session.services.TaskConstructors.Regular
	if compact {
		constructor = session.services.TaskConstructors.Compact
	}
	taskValue, turnValue, err := constructor(session.ctx, session, input, baseContext)
	if err != nil {
		session.rejectTurn(submissionID, turnID, err, false)
		return
	}
	if taskValue == nil {
		session.rejectTurn(submissionID, turnID, errors.New("session task constructor returned nil task"), false)
		return
	}
	turnContext := &turnValue
	abortPrepared := func() {
		abortCtx, cancel := context.WithTimeout(context.WithoutCancel(session.ctx), 2*time.Second)
		defer cancel()
		_ = taskValue.Abort(abortCtx, session, turnContext)
	}
	kind := protocol.TaskKindRegular
	if compact {
		kind = protocol.TaskKindCompact
	}
	startedEvent := protocol.TurnStartedEvent{
		ThreadID: session.threadID, TurnID: turnID, StartedAt: now, Input: input, Kind: kind,
	}
	items := []rollout.RolloutItem{turnContextItem(*turnContext)}
	if !compact {
		responseItem, responseErr := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: string(llm.RoleUser), Content: input})
		if responseErr != nil {
			abortPrepared()
			session.rejectTurn(submissionID, turnID, responseErr, false)
			return
		}
		items = append(items, responseItem)
	}
	items = append(items, rollout.EventMsgItem{Msg: startedEvent})
	if err := session.appendItemsDurable(session.ctx, turnID, items...); err != nil {
		abortPrepared()
		session.rejectTurn(submissionID, turnID, err, true)
		return
	}
	running, err := NewRunningTask(session.ctx, session, taskValue, turnContext, []TurnInput{{Content: input}})
	if err != nil {
		abortPrepared()
		session.completeWithoutTask(submissionID, turnID, err)
		return
	}
	session.active = &ActiveTurn{SubmissionID: submissionID, State: &turn.TurnState{StartedAt: now}, Task: running, pending: make(map[protocol.RequestID]chan protocol.Op)}
	session.publish(protocol.Event{ID: submissionID, Msg: startedEvent})
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

func (session *Session) rejectTurn(submissionID protocol.SubmissionID, turnID protocol.TurnID, err error, fatal bool) {
	if err == nil {
		err = errors.New("turn was rejected")
	}
	session.publish(protocol.Event{ID: submissionID, Msg: protocol.ErrorEvent{ThreadID: protocol.ThreadID(session.threadID), TurnID: protocol.TurnID(turnID), Code: "turn_start_failed", Message: err.Error(), At: session.services.Clock().UTC()}})
	if fatal {
		session.cancel(fmt.Errorf("start turn persistence: %w", err))
	}
}

func (session *Session) finishTurn(completion Completion) {
	if session.active == nil || session.active.Task.Context().TurnID != completion.TurnID {
		return
	}
	submissionID := session.active.SubmissionID
	completedItems := append([]rollout.RolloutItem(nil), completion.Result.Items...)
	if completion.Result.Usage.TotalTokens > 0 {
		if usageItem, usageErr := engine.UsageItem(completion.Result.Usage); usageErr == nil {
			completedItems = append(completedItems, usageItem)
		} else if completion.Error == nil {
			completion.Error = usageErr
		}
	}
	if len(completedItems) > 0 {
		cleanupCtx, cancel := session.cleanupContext()
		err := session.appendItemsDurable(cleanupCtx, completion.TurnID, completedItems...)
		cancel()
		if err != nil && completion.Error == nil {
			completion.Error = err
		}
		if err == nil {
			session.publishCompactionEvents(submissionID, completion.TurnID, completedItems)
		}
	}
	finishedAt := session.services.Clock().UTC()
	if errors.Is(completion.Cause, ErrInterrupted) || errors.Is(completion.Error, context.Canceled) && completion.Cause != nil {
		reason := completion.Cause.Error()
		summary := completion.Result.Summary
		if summary == "" {
			summary = "result: cancelled"
		}
		abortedEvent := protocol.TurnAbortedEvent{
			ThreadID: session.threadID, TurnID: completion.TurnID,
			Summary: summary, Reason: reason, FinishedAt: finishedAt,
		}
		cleanupCtx, cancel := session.cleanupContext()
		persistErr := session.appendItemsDurable(cleanupCtx, completion.TurnID, rollout.EventMsgItem{Msg: abortedEvent})
		cancel()
		session.active.State.MarkInterrupted(errors.Join(completion.Cause, persistErr))
		session.clearPendingRequests()
		session.active = nil
		if persistErr != nil {
			session.publish(protocol.Event{ID: submissionID, Msg: protocol.StreamErrorEvent{ThreadID: protocol.ThreadID(session.threadID), TurnID: protocol.TurnID(completion.TurnID), Message: persistErr.Error()}})
			session.cancel(fmt.Errorf("persist aborted turn: %w", persistErr))
			return
		}
		session.publish(protocol.Event{ID: submissionID, Msg: abortedEvent})
		return
	}
	status := protocol.TurnStatusCompleted
	outcome := completion.Result.Outcome
	if !outcome.Valid() {
		outcome = OutcomeCompleted
	}
	reason := strings.TrimSpace(completion.Result.Reason)
	errorText := ""
	if completion.Error != nil {
		status = protocol.TurnStatusFailed
		outcome = OutcomeFailed
		errorText = completion.Error.Error()
		if reason == "" {
			reason = errorText
		}
	} else if outcome == OutcomeFailed {
		status = protocol.TurnStatusFailed
		errorText = reason
		if errorText == "" {
			errorText = "task reported failed outcome"
		}
	}
	completedEvent := protocol.TurnCompleteEvent{
		ThreadID: session.threadID, TurnID: completion.TurnID,
		Status: status, Outcome: outcome, Reason: reason,
		Summary: completion.Result.Summary, Error: errorText, FinishedAt: finishedAt,
	}
	cleanupCtx, cancel := session.cleanupContext()
	persistErr := session.appendItemsDurable(cleanupCtx, completion.TurnID, rollout.EventMsgItem{Msg: completedEvent})
	cancel()
	if persistErr != nil {
		status = protocol.TurnStatusFailed
		errorText = errors.Join(completion.Error, persistErr).Error()
	}
	session.active.State.MarkTerminal(errors.Join(completion.Error, persistErr))
	session.clearPendingRequests()
	session.active = nil
	if persistErr != nil {
		session.publish(protocol.Event{ID: submissionID, Msg: protocol.StreamErrorEvent{ThreadID: protocol.ThreadID(session.threadID), TurnID: protocol.TurnID(completion.TurnID), Message: persistErr.Error()}})
		session.cancel(fmt.Errorf("persist completed turn: %w", persistErr))
		return
	}
	session.publish(protocol.Event{ID: submissionID, Msg: completedEvent})
}

func (session *Session) Mode() turn.ModeKind {
	if session == nil {
		return turn.ModeKindDefault
	}
	session.modeMu.RLock()
	defer session.modeMu.RUnlock()
	return session.state.Mode.Mode
}

func (session *Session) publishCompactionEvents(submissionID protocol.SubmissionID, turnID protocol.TurnID, items []rollout.RolloutItem) {
	for _, item := range items {
		if _, ok := item.(rollout.CompactedItem); !ok {
			continue
		}
		session.publish(protocol.Event{
			ID: submissionID,
			Msg: protocol.ContextCompactedEvent{
				ThreadID: protocol.ThreadID(session.threadID), TurnID: protocol.TurnID(turnID),
				ItemID: protocol.ItemID(fmt.Sprintf("compaction-%s", turnID)),
			},
		})
		session.publish(protocol.Event{
			ID:  submissionID,
			Msg: protocol.WarningEvent{ThreadID: protocol.ThreadID(session.threadID), TurnID: protocol.TurnID(turnID), Message: compactionWarningMessage},
		})
	}
}

func (session *Session) completeWithoutTask(submissionID protocol.SubmissionID, turnID protocol.TurnID, taskErr error) {
	completedEvent := protocol.TurnCompleteEvent{
		ThreadID: session.threadID, TurnID: turnID,
		Status: protocol.TurnStatusFailed, Outcome: protocol.TurnOutcomeFailed,
		Reason: taskErr.Error(), Error: taskErr.Error(), FinishedAt: session.services.Clock().UTC(),
	}
	cleanupCtx, cancel := session.cleanupContext()
	persistErr := session.appendItemsDurable(cleanupCtx, turnID, rollout.EventMsgItem{Msg: completedEvent})
	cancel()
	if persistErr != nil {
		session.publish(protocol.Event{ID: submissionID, Msg: protocol.StreamErrorEvent{ThreadID: protocol.ThreadID(session.threadID), TurnID: protocol.TurnID(turnID), Message: persistErr.Error()}})
		session.cancel(fmt.Errorf("persist rejected turn: %w", persistErr))
		return
	}
	session.publish(protocol.Event{ID: submissionID, Msg: completedEvent})
}

func (session *Session) cancelActive(cause error) {
	if session.active == nil {
		return
	}
	session.active.Task.Cancel(cause)
	abortCtx, cancel := context.WithTimeout(context.WithoutCancel(session.ctx), 2*time.Second)
	defer cancel()
	if err := session.active.Task.Abort(abortCtx); err != nil {
		session.publish(protocol.Event{ID: session.active.SubmissionID, Msg: protocol.WarningEvent{ThreadID: protocol.ThreadID(session.threadID), TurnID: protocol.TurnID(session.active.Task.Context().TurnID), Message: err.Error()}})
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
