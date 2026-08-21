package multiagent

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

const (
	ExplorerRole          = "explorer"
	defaultWaitTimeout    = 30 * time.Second
	maxStatusMessageRunes = 16_000
)

var agentNicknames = []string{
	"atlas", "curie", "darwin", "euclid", "faraday", "galileo", "hopper", "lovelace",
	"newton", "pascal", "raman", "tesla", "turing", "vesalius", "watt", "zeno",
}

type Options struct {
	MaxAgents int
	MaxDepth  int
}

func (options Options) Validate() error {
	if options.MaxAgents <= 0 {
		return errors.New("multi-agent max agents must be positive")
	}
	if options.MaxDepth != 1 {
		return errors.New("basic multi-agent max depth must be 1")
	}
	return nil
}

type SpawnChildRequest struct {
	ParentThreadID protocol.ThreadID
	Depth          int
	Nickname       string
	Role           string
	Message        string
}

type AgentRuntime interface {
	ID() protocol.ThreadID
	Submit(context.Context, protocol.Op) error
	SubmitUserInput(context.Context, protocol.UserInputOp) error
	Shutdown(context.Context) error
	Events() <-chan protocol.Event
	Terminated() <-chan struct{}
}

type AgentHost interface {
	SpawnChild(context.Context, *Control, SpawnChildRequest) (AgentRuntime, error)
	NotifyParent(context.Context, protocol.ThreadID, Notification) error
}

type Notification struct {
	Metadata protocol.AgentMetadata
	Status   protocol.AgentStatus
}

type AgentRecord struct {
	Metadata protocol.AgentMetadata
	Status   protocol.AgentStatus
}

type SpawnResult struct {
	AgentID  protocol.ThreadID `json:"agent_id"`
	Nickname string            `json:"nickname"`
}

type StatusSnapshot struct {
	AgentID  protocol.ThreadID    `json:"agent_id"`
	Nickname string               `json:"nickname,omitempty"`
	Role     string               `json:"role,omitempty"`
	Status   protocol.AgentStatus `json:"status"`
}

type WaitResult struct {
	Statuses []StatusSnapshot `json:"statuses"`
	TimedOut bool             `json:"timed_out"`
}

type record struct {
	metadata         protocol.AgentMetadata
	status           protocol.AgentStatus
	runtime          AgentRuntime
	latestAssistant  string
	notifiedTurn     bool
	notifiedShutdown bool
	closing          bool
}

type reservation struct {
	nickname string
}

type Control struct {
	host      AgentHost
	rootID    protocol.ThreadID
	maxAgents int
	maxDepth  int

	mu           sync.Mutex
	agents       map[protocol.ThreadID]*record
	reservations map[string]reservation
	nicknames    map[string]struct{}
	changed      chan struct{}
	done         chan struct{}
	closed       bool
	closeOnce    sync.Once
	closeErr     error
}
