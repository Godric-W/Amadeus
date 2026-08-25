package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/thread"
)

type Configuration struct {
	Runtime            config.Config
	Source             protocol.SessionSource
	CWD                string
	WorkspaceRoots     []string
	AmadeusRoot        string
	Shell              string
	CurrentDate        string
	Timezone           string
	Mode               turn.ModeKind
	Personality        turn.Personality
	OutputSchema       json.RawMessage
	OutputSchemaStrict bool
}

type SessionState struct {
	Configuration Configuration
	Context       *agentcontext.Manager
}

type SessionIo struct {
	Submissions   chan<- protocol.Submission
	Events        <-chan protocol.Event
	Terminated    <-chan struct{}
	Configured    <-chan error
	admissions    *pendingUserMessageAdmissions
	steerRequests chan<- steerInputRequest
}

type SpawnArgs struct {
	SessionID      protocol.SessionID
	ThreadID       protocol.ThreadID
	ParentThreadID *protocol.ThreadID
	History        thread.InitialHistory
	State          SessionState
	Services       SessionServices
	Adapters       ServiceAdapters
}

type ActiveTurn struct {
	SubmissionID protocol.SubmissionID
	Task         *RunningTask
	State        *TurnState
	Output       TaskOutput
}

type Session struct {
	sessionID      protocol.SessionID
	threadID       protocol.ThreadID
	parentThreadID *protocol.ThreadID
	state          SessionState
	configMu       sync.RWMutex
	services       SessionServices
	active         *ActiveTurn
	deferred       []protocol.Submission
	inputQueue     InputQueue
	appendMu       sync.Mutex
	admissions     *pendingUserMessageAdmissions

	ctx           context.Context
	cancel        context.CancelCauseFunc
	submissions   chan protocol.Submission
	events        chan protocol.Event
	terminated    chan struct{}
	configured    chan error
	completed     chan Completion
	requestsIn    chan requestDelivery
	steerRequests chan steerInputRequest
}

const compactionWarningMessage = "Heads up: Long threads and multiple compactions can cause the model to be less accurate. Start a new thread when possible to keep threads small and targeted."

var ErrInterrupted = errors.New("turn interrupted by user")

func Spawn(parent context.Context, args SpawnArgs) (*Session, SessionIo, error) {
	if parent == nil || args.Services.LiveThread == nil || args.Services.NextID == nil || !args.Adapters.configured() {
		return nil, SessionIo{}, errors.New("session spawn arguments are incomplete")
	}
	if args.Services.Clock == nil {
		args.Services.Clock = time.Now
	}
	if args.State.Configuration.Source.Kind == "" {
		args.State.Configuration.Source = protocol.RootSessionSource()
	}
	if err := args.State.Configuration.Source.Validate(); err != nil {
		return nil, SessionIo{}, fmt.Errorf("validate session source: %w", err)
	}
	if err := args.History.Validate(args.ThreadID); err != nil {
		return nil, SessionIo{}, fmt.Errorf("validate initial history: %w", err)
	}
	if err := validateSpawnIdentity(args); err != nil {
		return nil, SessionIo{}, err
	}
	ctx, cancel := context.WithCancelCause(parent)
	args.State.Configuration = cloneConfiguration(args.State.Configuration)
	closeSpawnServices := func() {
		_ = args.Services.Close()
		_ = args.Services.LiveThread.Shutdown(context.Background())
	}
	value := &Session{
		sessionID: args.SessionID, threadID: args.ThreadID, parentThreadID: cloneOptionalThreadID(args.ParentThreadID),
		state: args.State, services: args.Services, ctx: ctx, cancel: cancel,
		submissions: make(chan protocol.Submission, 32), events: make(chan protocol.Event, 128),
		terminated: make(chan struct{}), configured: make(chan error, 1), completed: make(chan Completion, 1),
		requestsIn: make(chan requestDelivery, 8), steerRequests: make(chan steerInputRequest, 8),
		admissions: newPendingUserMessageAdmissions(),
	}
	contextManager, err := agentcontext.NewManagerFromRollout(args.History.Lines, nil)
	if err != nil {
		cancel(err)
		closeSpawnServices()
		return nil, SessionIo{}, fmt.Errorf("rebuild context from initial history: %w", err)
	}
	value.state.Context = contextManager
	if args.Adapters.configured() {
		capabilities, buildErr := buildSessionServices(ctx, value, value.services, args.Adapters)
		if buildErr != nil {
			cancel(buildErr)
			closeSpawnServices()
			return nil, SessionIo{}, fmt.Errorf("build session services: %w", buildErr)
		}
		value.services = capabilities
	}
	io := SessionIo{
		Submissions: value.submissions, Events: value.events, Terminated: value.terminated, Configured: value.configured,
		admissions: value.admissions, steerRequests: value.steerRequests,
	}
	go value.loop()
	return value, io, nil
}

func (session *Session) loop() {
	defer session.admissions.failAll(errors.New("session terminated before user message admission"))
	defer close(session.events)
	defer session.publish(protocol.Event{Msg: protocol.ShutdownCompleteEvent{ThreadID: session.threadID}})
	defer close(session.terminated)
	defer func() {
		session.clearPendingRequests()
	}()
	defer func() {
		cleanupCtx, cancel := session.cleanupContext()
		defer cancel()
		_ = session.services.LiveThread.Shutdown(cleanupCtx)
	}()
	defer func() { _ = session.services.Close() }()
	configuredErr := session.Publish(session.ctx, protocol.Event{Msg: protocol.SessionConfiguredEvent{
		SessionID: session.sessionID, ThreadID: session.threadID, ParentThreadID: cloneOptionalThreadID(session.parentThreadID),
		Configuration: session.ProtocolConfiguration(),
	}})
	session.configured <- configuredErr
	close(session.configured)
	if configuredErr != nil {
		return
	}
	sessionDone := session.ctx.Done()
	for {
		if session.active == nil && len(session.deferred) > 0 {
			next := session.deferred[0]
			session.deferred = session.deferred[1:]
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
		case request := <-session.steerRequests:
			turnID, err := session.steerInput(request.input, request.expectedTurnID)
			request.result <- steerInputResult{turnID: turnID, err: err}
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
		session.admissions.complete(submission.ID, userMessageAdmissionResult{err: err})
		if _, userInput := submission.Op.(protocol.UserInputOp); !userInput {
			session.publish(protocol.Event{ID: submission.ID, Msg: protocol.ErrorEvent{ThreadID: session.threadID, Code: "invalid_submission", Message: err.Error(), At: session.services.Clock().UTC()}})
		}
		return
	}
	switch op := submission.Op.(type) {
	case protocol.UserInputOp:
		admission, err := session.admitUserMessage(submission.ID, op)
		session.admissions.complete(submission.ID, userMessageAdmissionResult{admission: admission, err: err})
	case protocol.CompactOp:
		if session.active != nil {
			session.deferred = append(session.deferred, submission)
			return
		}
		_, _ = session.startTurn(submission.ID, "compact context", "", TaskKindCompact)
	case protocol.InterruptOp:
		session.cancelActive(ErrInterrupted)
	case protocol.ThreadSettingsOp:
		if session.active != nil {
			session.deferred = append(session.deferred, submission)
			return
		}
		if op.Mode.Valid() {
			session.setMode(turn.ModeKind(op.Mode))
			session.publish(protocol.Event{ID: submission.ID, Msg: protocol.ThreadSettingsAppliedEvent{
				ThreadID: session.threadID, Configuration: session.ProtocolConfiguration(),
			}})
		}
	case protocol.ApprovalDecisionOp:
		session.resolveRequest(op.RequestID, op)
	case protocol.UserInputAnswerOp:
		session.resolveRequest(op.RequestID, op)
	}
}

func (session *Session) admitUserMessage(submissionID protocol.SubmissionID, op protocol.UserInputOp) (protocol.UserMessageAdmission, error) {
	content := strings.TrimSpace(op.Content)
	if content == "" {
		return protocol.UserMessageAdmission{}, errors.New("user input is empty")
	}
	if op.ThreadSettings.CollaborationMode != nil {
		mode := op.ThreadSettings.CollaborationMode.Mode
		if !mode.Valid() {
			return protocol.UserMessageAdmission{}, fmt.Errorf("collaboration mode %q is invalid", mode)
		}
		session.setMode(turn.ModeKind(mode))
		session.publish(protocol.Event{ID: submissionID, Msg: protocol.ThreadSettingsAppliedEvent{
			ThreadID: session.threadID, Configuration: session.ProtocolConfiguration(),
		}})
	}
	turnID, err := session.steerInput(UserTurnInput{Content: content, ClientID: strings.TrimSpace(op.ClientUserMessageID)}, "")
	if err == nil {
		return protocol.UserMessageAdmission{Kind: protocol.UserMessageAdmissionSteered, TurnID: turnID}, nil
	}
	if !isSteerInputError(err, SteerInputNoActiveTurn) {
		return protocol.UserMessageAdmission{}, err
	}
	turnID, err = session.startTurn(submissionID, content, strings.TrimSpace(op.ClientUserMessageID), TaskKindRegular)
	if err != nil {
		return protocol.UserMessageAdmission{}, err
	}
	return protocol.UserMessageAdmission{Kind: protocol.UserMessageAdmissionStarted, TurnID: turnID}, nil
}

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

func (session *Session) startTurn(submissionID protocol.SubmissionID, input, clientUserMessageID string, kind TaskKind) (protocol.TurnID, error) {
	if input == "" {
		return "", errors.New("turn input is empty")
	}
	now := session.services.Clock().UTC()
	turnID := protocol.TurnID(session.services.NextID("turn"))
	configuration := session.Configuration()
	baseContext := turn.TurnContext{
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
	createInput := thread.CreateInput{
		SessionID: session.sessionID, ID: session.threadID, Source: configuration.Source.Clone(), CWD: configuration.CWD, Title: titleFromInput(input),
		ModelProvider: configuration.Runtime.ModelProvider, Model: configuration.Runtime.Model, CreatedAt: now,
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
		session.rejectTurn(submissionID, turnID, errors.New("session task constructor returned nil task"), false)
		return turnID, errors.New("session task constructor returned nil task")
	}
	turnContext := &turnValue
	startedEvent := protocol.TurnStartedEvent{
		ThreadID: session.threadID, TurnID: turnID, StartedAt: now,
	}
	items := []rollout.RolloutItem{turnContextItem(*turnContext)}
	var userItem protocol.TurnItem
	if kind == TaskKindRegular {
		responseItem, responseErr := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: string(llm.RoleUser), Content: input})
		if responseErr != nil {
			session.rejectTurn(submissionID, turnID, responseErr, false)
			return turnID, responseErr
		}
		items = append(items, responseItem)
		userItem = completedUserMessageItem(protocol.ItemID(session.services.NextID("item")), input, clientUserMessageID, now)
		items = append(items, rollout.EventMsgItem{Msg: protocol.ItemCompletedEvent{ThreadID: session.threadID, TurnID: turnID, Item: userItem}})
	}
	items = append(items, rollout.EventMsgItem{Msg: startedEvent})
	if err := session.appendItemsDurable(session.ctx, turnID, items...); err != nil {
		session.rejectTurn(submissionID, turnID, err, true)
		return turnID, err
	}
	running, err := NewRunningTask(session.ctx, session, taskValue, turnContext)
	if err != nil {
		session.completeWithoutTask(submissionID, turnID, err)
		return turnID, err
	}
	session.active = &ActiveTurn{SubmissionID: submissionID, Task: running, State: turnState}
	session.publish(protocol.Event{ID: submissionID, Msg: startedEvent})
	if kind == TaskKindRegular {
		session.publish(protocol.Event{ID: submissionID, Msg: protocol.ItemCompletedEvent{ThreadID: session.threadID, TurnID: turnID, Item: userItem}})
	}
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
	if session.active == nil {
		return
	}
	session.active.Task.Cancel(cause)
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
