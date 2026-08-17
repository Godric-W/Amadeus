package turn

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type ID = rollout.TurnID

type PermissionMode string

const (
	PermissionModeDefault PermissionMode = "default"
	PermissionModePlan    PermissionMode = "plan"
)

type Personality string

type Context struct {
	ThreadID rollout.ThreadID `json:"thread_id"`
	TurnID   ID               `json:"turn_id"`
	Provider string           `json:"provider"`
	Model    string           `json:"model"`
	CWD      string           `json:"cwd"`
	Shell    string           `json:"shell,omitempty"`

	CurrentDate string `json:"current_date,omitempty"`
	Timezone    string `json:"timezone,omitempty"`

	InitialPermissionMode PermissionMode  `json:"initial_permission_mode"`
	Personality           Personality     `json:"personality,omitempty"`
	OutputSchema          json.RawMessage `json:"output_schema,omitempty"`
}

func (value Context) Validate() error {
	if value.ThreadID == "" || value.TurnID == "" || strings.TrimSpace(value.Provider) == "" || strings.TrimSpace(value.Model) == "" || strings.TrimSpace(value.CWD) == "" {
		return errors.New("turn context is incomplete")
	}
	if value.InitialPermissionMode != PermissionModeDefault && value.InitialPermissionMode != PermissionModePlan {
		return errors.New("turn permission mode is invalid")
	}
	if len(value.OutputSchema) > 0 && !json.Valid(value.OutputSchema) {
		return errors.New("turn output schema is invalid")
	}
	return nil
}

type State struct {
	mu            sync.RWMutex
	StartedAt     time.Time
	Usage         llm.Usage
	ToolCallCount int
	Terminal      bool
	TerminalError string
}

func (state *State) Snapshot() StateSnapshot {
	if state == nil {
		return StateSnapshot{}
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	return StateSnapshot{
		StartedAt: state.StartedAt, Usage: state.Usage, ToolCallCount: state.ToolCallCount,
		Terminal: state.Terminal, TerminalError: state.TerminalError,
	}
}

func (state *State) Record(usage llm.Usage, toolCalls int) {
	if state == nil {
		return
	}
	state.mu.Lock()
	state.Usage.InputTokens += usage.InputTokens
	state.Usage.CachedInputTokens += usage.CachedInputTokens
	state.Usage.OutputTokens += usage.OutputTokens
	state.Usage.ReasoningTokens += usage.ReasoningTokens
	state.Usage.TotalTokens += usage.TotalTokens
	state.ToolCallCount += toolCalls
	state.mu.Unlock()
}

func (state *State) MarkTerminal(err error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.Terminal = true
	if err != nil {
		state.TerminalError = err.Error()
	}
}

type StateSnapshot struct {
	StartedAt     time.Time
	Usage         llm.Usage
	ToolCallCount int
	Terminal      bool
	TerminalError string
}
