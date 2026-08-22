package app

import (
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/policy"
)

type ThreadViewSnapshot struct {
	Generation    uint64
	SessionID     protocol.SessionID
	ThreadID      protocol.ThreadID
	Title         string
	Configuration protocol.SessionConfiguration
	Items         []protocol.TurnItem
	Usage         llm.Usage
	ContextWindow int64
}

type SessionOption struct {
	ID      protocol.ThreadID
	Title   string
	Current bool
}

type SkillOption struct {
	Name        string
	Description string
	Source      string
	Path        string
	Enabled     bool
}

type MCPDetail string

const (
	MCPDetailSummary MCPDetail = "summary"
	MCPDetailVerbose MCPDetail = "verbose"
)

type MCPAuthStatus string

const (
	MCPAuthUnknown     MCPAuthStatus = "Unknown"
	MCPAuthUnsupported MCPAuthStatus = "Unsupported"
	MCPAuthBearerToken MCPAuthStatus = "Bearer token"
)

type MCPServerStatus struct {
	Name       string
	Enabled    bool
	AuthStatus MCPAuthStatus
	Tools      []mcp.MCPToolMetadata
	Resources  []mcp.MCPResourceMetadata
	Error      string
}

type MCPInventory struct {
	Servers []MCPServerStatus
}

type StatusSnapshot struct {
	SessionID            protocol.SessionID
	ThreadID             protocol.ThreadID
	Title                string
	CurrentDir           string
	Provider             string
	Model                string
	ReasoningEffort      *llm.ReasoningEffort
	Mode                 turn.ModeKind
	Phase                string
	Usage                llm.Usage
	ContextWindow        int64
	RolloutItems         int
	PermissionGrantCount int
	SkillRevision        string
	MCPRevision          string
}

type InteractiveEvent interface{ isInteractiveEvent() }

type SessionEventObserved struct {
	Generation uint64
	Event      protocol.Event
}

func (SessionEventObserved) isInteractiveEvent() {}

type ApprovalRequested struct {
	Generation uint64
	RequestID  string
	Request    policy.ApprovalRequest
}

func (ApprovalRequested) isInteractiveEvent() {}

type UserInputRequested struct {
	Generation uint64
	Request    protocol.RequestUserInputEvent
}

func (UserInputRequested) isInteractiveEvent() {}

type ThreadAttached struct{ Snapshot ThreadViewSnapshot }

func (ThreadAttached) isInteractiveEvent() {}

type ThreadAttachFailed struct{ Error error }

func (ThreadAttachFailed) isInteractiveEvent() {}

type SessionsLoaded struct {
	Sessions []SessionOption
	Error    error
}

func (SessionsLoaded) isInteractiveEvent() {}

type ThreadNameUpdated struct {
	Generation uint64
	ThreadID   protocol.ThreadID
	Name       string
}

func (ThreadNameUpdated) isInteractiveEvent() {}

type ThreadRenameFailed struct{ Error error }

func (ThreadRenameFailed) isInteractiveEvent() {}

type ThreadDeleted struct{ ThreadID protocol.ThreadID }

func (ThreadDeleted) isInteractiveEvent() {}

type ThreadDeleteFailed struct{ Error error }

func (ThreadDeleteFailed) isInteractiveEvent() {}

type ClearUIStarted struct{}

func (ClearUIStarted) isInteractiveEvent() {}

type MCPInventoryLoaded struct {
	RequestID  uint64
	Generation uint64
	ThreadID   protocol.ThreadID
	Detail     MCPDetail
	Inventory  MCPInventory
	Error      error
}

func (MCPInventoryLoaded) isInteractiveEvent() {}

type SkillsLoaded struct {
	Generation uint64
	Skills     []SkillOption
	Error      error
}

func (SkillsLoaded) isInteractiveEvent() {}

type SkillEnabledSet struct {
	Generation uint64
	Path       string
	Enabled    bool
	Error      error
}

func (SkillEnabledSet) isInteractiveEvent() {}

type ApplicationError struct {
	Operation string
	Error     error
}

func (ApplicationError) isInteractiveEvent() {}
