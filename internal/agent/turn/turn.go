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

type ModeKind string

const (
	ModeKindDefault ModeKind = "default"
	ModeKindPlan    ModeKind = "plan"
)

type Personality string

type TurnContext struct {
	ThreadID rollout.ThreadID `json:"thread_id"`
	TurnID   ID               `json:"turn_id"`
	Provider string           `json:"provider"`
	Model    string           `json:"model"`
	CWD      string           `json:"cwd"`
	Shell    string           `json:"shell,omitempty"`

	CurrentDate string `json:"current_date,omitempty"`
	Timezone    string `json:"timezone,omitempty"`

	Mode         ModeKind        `json:"mode"`
	Personality  Personality     `json:"personality,omitempty"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
}

func (value TurnContext) Validate() error {
	if value.ThreadID == "" || value.TurnID == "" || strings.TrimSpace(value.Provider) == "" || strings.TrimSpace(value.Model) == "" || strings.TrimSpace(value.CWD) == "" {
		return errors.New("turn context is incomplete")
	}
	if value.Mode != ModeKindDefault && value.Mode != ModeKindPlan {
		return errors.New("turn mode is invalid")
	}
	if len(value.OutputSchema) > 0 && !json.Valid(value.OutputSchema) {
		return errors.New("turn output schema is invalid")
	}
	return nil
}

type TurnState struct {
	mu            sync.RWMutex
	StartedAt     time.Time
	Usage         llm.Usage
	ToolCallCount int
	Terminal      bool
	Interrupted   bool
	TerminalError string
}

func (state *TurnState) Snapshot() StateSnapshot {
	if state == nil {
		return StateSnapshot{}
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	return StateSnapshot{
		StartedAt: state.StartedAt, Usage: state.Usage, ToolCallCount: state.ToolCallCount,
		Terminal: state.Terminal, Interrupted: state.Interrupted, TerminalError: state.TerminalError,
	}
}

func (state *TurnState) Record(usage llm.Usage, toolCalls int) {
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

func (state *TurnState) MarkTerminal(err error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.Terminal = true
	if err != nil {
		state.TerminalError = err.Error()
	}
}

func (state *TurnState) MarkInterrupted(err error) {
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.Interrupted = true
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
	Interrupted   bool
	TerminalError string
}
