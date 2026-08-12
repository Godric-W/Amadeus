package react

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type StopReason string

const (
	StopCompleted       StopReason = "completed"
	StopStalled         StopReason = "stalled"
	StopBlocked         StopReason = "blocked"
	StopFailed          StopReason = "failed"
	StopInterrupted     StopReason = "interrupted"
	StopBudgetExhausted StopReason = "budget_exhausted"
)

type Budget struct {
	MaxIterations int
	MaxToolCalls  int
	MaxDuration   time.Duration
}

type BudgetState struct {
	Budget           Budget
	IterationsUsed   int
	ToolCallsUsed    int
	InputTokensUsed  int64
	OutputTokensUsed int64
	Elapsed          time.Duration
}

func (state BudgetState) Validate() error {
	if state.Budget.MaxIterations < 0 || state.Budget.MaxToolCalls < 0 || state.Budget.MaxDuration < 0 {
		return errors.New("Reactor budget limits cannot be negative")
	}
	if state.IterationsUsed < 0 || state.ToolCallsUsed < 0 || state.InputTokensUsed < 0 || state.OutputTokensUsed < 0 || state.Elapsed < 0 {
		return errors.New("Reactor budget usage cannot be negative")
	}
	return nil
}

type LimitKind string

const (
	LimitIterations LimitKind = "iterations"
	LimitToolCalls  LimitKind = "tool_calls"
	LimitWallClock  LimitKind = "wall_clock"
)

type LimitReached struct {
	Limit   LimitKind
	Used    int64
	Maximum int64
}

type ToolOutcomeStatus string

const (
	ToolOutcomeSucceeded   ToolOutcomeStatus = "succeeded"
	ToolOutcomeFailed      ToolOutcomeStatus = "failed"
	ToolOutcomeDenied      ToolOutcomeStatus = "denied"
	ToolOutcomeInterrupted ToolOutcomeStatus = "interrupted"
)

type ArtifactRef struct {
	Path   string
	URI    string
	Digest string
}

func (status ToolOutcomeStatus) Valid() bool {
	switch status {
	case ToolOutcomeSucceeded, ToolOutcomeFailed, ToolOutcomeDenied, ToolOutcomeInterrupted:
		return true
	default:
		return false
	}
}

type ToolError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type ToolOutcome struct {
	CallID    string            `json:"call_id"`
	ToolName  string            `json:"tool_name"`
	Status    ToolOutcomeStatus `json:"status"`
	Result    tool.Output       `json:"result"`
	Error     *ToolError        `json:"error,omitempty"`
	Blocking  bool              `json:"blocking,omitempty"`
	Partial   bool              `json:"partial,omitempty"`
	Duration  time.Duration     `json:"duration"`
	Artifacts []ArtifactRef     `json:"artifacts,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
}

func (outcome ToolOutcome) Succeeded() bool { return outcome.Status == ToolOutcomeSucceeded }

func (outcome ToolOutcome) ErrorMessage() string {
	if outcome.Error == nil {
		return ""
	}
	return outcome.Error.Message
}

type IterationStatus string

const (
	IterationRunning     IterationStatus = "running"
	IterationCompleted   IterationStatus = "completed"
	IterationFailed      IterationStatus = "failed"
	IterationInterrupted IterationStatus = "interrupted"
)

type Iteration struct {
	Index       int
	LLMCallID   string
	Intent      string
	ToolCalls   []tool.ToolCall
	Outcomes    []ToolOutcome
	Status      IterationStatus
	StartedAt   time.Time
	CompletedAt *time.Time
}

type LoopState struct {
	Budget     BudgetState
	Iterations []Iteration
	Usage      llm.Usage
}

func newLoopState(request Request) LoopState {
	return LoopState{
		Budget: request.Budget, Iterations: append([]Iteration(nil), request.PriorIterations...),
	}
}

type Request struct {
	TurnID           string
	Goal             string
	Context          *agentcontext.Manager
	BaseInstructions llm.BaseInstructions
	ModelInfo        llm.ModelInfo
	AvailableTools   []tool.Spec
	OutputSchema     llm.OutputSchema
	RequestSnapshot  tool.RequestSnapshot
	BeforeSample     func(context.Context, *agentcontext.Manager) error
	PriorIterations  []Iteration
	Budget           BudgetState
}

func (request Request) Validate() error {
	if strings.TrimSpace(request.TurnID) == "" {
		return errors.New("Reactor request TurnID is empty")
	}
	if strings.TrimSpace(request.Goal) == "" {
		return errors.New("Reactor request goal is empty")
	}
	if request.Context == nil {
		return errors.New("Reactor request ContextManager is nil")
	}
	if strings.TrimSpace(request.BaseInstructions.Text) == "" {
		return errors.New("Reactor request BaseInstructions are empty")
	}
	if request.ModelInfo.MaxOutputTokens <= 0 {
		return errors.New("Reactor request ModelInfo max output tokens must be greater than zero")
	}
	if err := request.Budget.Validate(); err != nil {
		return err
	}
	return nil
}

type Result struct {
	FinalMessage *llm.Message
	Iterations   []Iteration
	Usage        llm.Usage
	Budget       BudgetState
	StopReason   StopReason
	Reason       string
	Limit        *LimitReached
}

func (result Result) Validate() error {
	switch result.StopReason {
	case StopCompleted:
		if result.FinalMessage == nil || strings.TrimSpace(result.FinalMessage.Content) == "" {
			return errors.New("completed Reactor result requires a final message")
		}
		if result.Limit != nil {
			return errors.New("completed Reactor result cannot have a budget limit")
		}
	case StopStalled, StopBlocked, StopFailed, StopInterrupted:
		if strings.TrimSpace(result.Reason) == "" {
			return fmt.Errorf("%s Reactor result requires a reason", result.StopReason)
		}
	case StopBudgetExhausted:
		if result.Limit == nil || result.Limit.Maximum <= 0 || result.Limit.Used < 0 {
			return errors.New("budget_exhausted Reactor result requires limit details")
		}
	default:
		return fmt.Errorf("invalid Reactor stop reason %q", result.StopReason)
	}
	return result.Budget.Validate()
}
