package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/contextmanager"
	"github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/threadstore"
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
	Mode               ModeKind
	Personality        Personality
	OutputSchema       json.RawMessage
	OutputSchemaStrict bool
}

type SessionState struct {
	Configuration Configuration
	Base          llm.BaseInstructions
	Context       *contextmanager.Manager
}

type SessionIo struct {
	Submissions         chan<- protocol.Submission
	Events              <-chan protocol.Event
	Terminated          <-chan struct{}
	Configured          <-chan error
	admissions          *pendingUserMessageAdmissions
	steerRequests       chan<- steerInputRequest
	startIfIdleRequests chan<- startIfIdleRequest
}

type SpawnArgs struct {
	SessionID      protocol.SessionID
	ThreadID       protocol.ThreadID
	ParentThreadID *protocol.ThreadID
	History        threadstore.InitialHistory
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
	sessionID        protocol.SessionID
	threadID         protocol.ThreadID
	parentThreadID   *protocol.ThreadID
	state            SessionState
	configMu         sync.RWMutex
	services         SessionServices
	active           *ActiveTurn
	deferred         []protocol.Submission
	inputQueue       InputQueue
	appendMu         sync.Mutex
	eventDeliveryMu  sync.Mutex
	eventDeliveryErr error
	admissions       *pendingUserMessageAdmissions

	ctx                 context.Context
	cancel              context.CancelCauseFunc
	discardOnExit       atomic.Bool
	submissions         chan protocol.Submission
	events              chan protocol.Event
	terminated          chan struct{}
	configured          chan error
	completed           chan Completion
	requestsIn          chan requestDelivery
	steerRequests       chan steerInputRequest
	startIfIdleRequests chan startIfIdleRequest
	injectTurnInput     chan injectTurnInputRequest
	extensionEventInbox chan extension.EventDelivery
	extensionBinding    extension.EventBinding
	idleLifecycle       chan extension.ThreadIdleCause
	idleDone            chan struct{}
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
	if args.Services.Extensions == nil {
		args.Services.Extensions = extension.EmptyRegistry()
	}
	sessionExtensions, err := extension.NewData(args.SessionID.String())
	if err != nil {
		cancel(err)
		return nil, SessionIo{}, err
	}
	threadExtensions, err := extension.NewData(args.ThreadID.String())
	if err != nil {
		cancel(err)
		return nil, SessionIo{}, err
	}
	args.Services.sessionExtensions = sessionExtensions
	args.Services.threadExtensions = threadExtensions
	closeSpawnServices := func() {
		_ = args.Services.Close()
		_ = args.Services.LiveThread.Discard(context.Background())
	}
	value := &Session{
		sessionID: args.SessionID, threadID: args.ThreadID, parentThreadID: cloneOptionalThreadID(args.ParentThreadID),
		state: args.State, services: args.Services, ctx: ctx, cancel: cancel,
		submissions: make(chan protocol.Submission, 32), events: make(chan protocol.Event, 128),
		terminated: make(chan struct{}), configured: make(chan error, 1), completed: make(chan Completion, 1),
		requestsIn: make(chan requestDelivery, 8), steerRequests: make(chan steerInputRequest, 8),
		startIfIdleRequests: make(chan startIfIdleRequest, 8),
		injectTurnInput:     make(chan injectTurnInputRequest, 8), extensionEventInbox: make(chan extension.EventDelivery, 128),
		idleLifecycle: make(chan extension.ThreadIdleCause, 1), idleDone: make(chan struct{}),
		admissions: newPendingUserMessageAdmissions(),
	}
	contextManager, err := contextmanager.NewManagerFromRollout(args.History.Lines, nil)
	if err != nil {
		cancel(err)
		closeSpawnServices()
		return nil, SessionIo{}, fmt.Errorf("rebuild context from initial history: %w", err)
	}
	value.state.Context = contextManager
	capabilities, buildErr := buildSessionServices(ctx, value, value.services, args.Adapters)
	if buildErr != nil {
		cancel(buildErr)
		closeSpawnServices()
		return nil, SessionIo{}, fmt.Errorf("build session services: %w", buildErr)
	}
	value.services = capabilities
	extension.Set[ThreadAutomation](value.services.threadExtensions, ThreadAutomation(&threadAutomation{session: value}))
	if binder, ok := value.services.extensionRegistry().EventSink().(extension.EventBinder); ok {
		binding, bindErr := binder.Bind(value.threadID, value.extensionEventInbox, value.terminated)
		if bindErr != nil {
			cancel(bindErr)
			closeSpawnServices()
			return nil, SessionIo{}, fmt.Errorf("bind extension event sink: %w", bindErr)
		}
		value.extensionBinding = binding
	}
	base, err := resolveSessionBase(args.History, value.state.Base, &value.services, value.state.Configuration.Personality)
	if err != nil {
		cancel(err)
		closeSpawnServices()
		return nil, SessionIo{}, fmt.Errorf("resolve session base instructions: %w", err)
	}
	value.state.Base = base
	if err := value.emitThreadStartLifecycle(ctx); err != nil {
		cancel(err)
		_ = value.emitThreadStopLifecycle(context.WithoutCancel(ctx))
		if value.extensionBinding != nil {
			value.extensionBinding.Close()
		}
		closeSpawnServices()
		return nil, SessionIo{}, fmt.Errorf("start thread extensions: %w", err)
	}
	io := SessionIo{
		Submissions: value.submissions, Events: value.events, Terminated: value.terminated, Configured: value.configured,
		admissions: value.admissions, steerRequests: value.steerRequests,
		startIfIdleRequests: value.startIfIdleRequests,
	}
	go value.runIdleLifecycle()
	go value.loop()
	return value, io, nil
}

// AbortInitialization stops a session that has not crossed its configured
// ownership barrier. Session cleanup then discards uncommitted writer bytes
// instead of applying normal shutdown flush semantics.
func (session *Session) AbortInitialization(ctx context.Context, cause error) error {
	if session == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("session initialization abort context is nil")
	}
	if cause == nil {
		cause = errors.New("session initialization aborted")
	}
	session.discardOnExit.Store(true)
	session.cancel(cause)
	select {
	case <-session.terminated:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
