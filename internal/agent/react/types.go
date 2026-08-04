package react

import (
	"errors"
	"fmt"
	"strings"
	"time"

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
	MaxIterations   int
	MaxToolCalls    int
	MaxInputTokens  int64
	MaxOutputTokens int64
	MaxDuration     time.Duration
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
	if state.Budget.MaxIterations < 0 || state.Budget.MaxToolCalls < 0 || state.Budget.MaxInputTokens < 0 || state.Budget.MaxOutputTokens < 0 || state.Budget.MaxDuration < 0 {
		return errors.New("Reactor budget limits cannot be negative")
	}
	if state.IterationsUsed < 0 || state.ToolCallsUsed < 0 || state.InputTokensUsed < 0 || state.OutputTokensUsed < 0 || state.Elapsed < 0 {
		return errors.New("Reactor budget usage cannot be negative")
	}
	return nil
}

type LimitKind string

const (
	LimitIterations   LimitKind = "iterations"
	LimitToolCalls    LimitKind = "tool_calls"
	LimitInputTokens  LimitKind = "input_tokens"
	LimitOutputTokens LimitKind = "output_tokens"
	LimitWallClock    LimitKind = "wall_clock"
)

type LimitReached struct {
	Limit   LimitKind
	Used    int64
	Maximum int64
}

type EvidenceID string
type EvidenceKind string

const (
	EvidenceTool       EvidenceKind = "tool"
	EvidenceFile       EvidenceKind = "file"
	EvidenceCommand    EvidenceKind = "command"
	EvidenceTest       EvidenceKind = "test"
	EvidenceDiagnostic EvidenceKind = "diagnostic"
)

type ArtifactRef struct {
	Path   string
	URI    string
	Digest string
}

type Evidence struct {
	ID           EvidenceID
	Kind         EvidenceKind
	Source       string
	Summary      string
	CriterionIDs []string
	Artifact     *ArtifactRef
	Verified     bool
}

type Observation struct {
	CallID   string
	ToolName string
	Result   tool.Result
	Error    string
	Blocking bool
	Duration time.Duration
}

type IterationStatus string

const (
	IterationRunning     IterationStatus = "running"
	IterationCompleted   IterationStatus = "completed"
	IterationFailed      IterationStatus = "failed"
	IterationInterrupted IterationStatus = "interrupted"
)

type Iteration struct {
	Index        int
	LLMCallID    string
	Intent       string
	ToolCalls    []tool.Call
	Observations []Observation
	Evidence     []Evidence
	Status       IterationStatus
	StartedAt    time.Time
	CompletedAt  *time.Time
}

type ExecutionMetadata struct {
	TaskID string
}

type LoopState struct {
	Budget          BudgetState
	RuntimeMessages []llm.Message
	Iterations      []Iteration
	Evidence        []Evidence
	Usage           llm.Usage
	PreviousUsage   *llm.Usage
	LastSentCount   int
}

func newLoopState(request Request) LoopState {
	return LoopState{
		Budget:          request.Budget,
		RuntimeMessages: make([]llm.Message, 0),
		Iterations:      append([]Iteration(nil), request.PriorIterations...),
		Evidence:        append([]Evidence(nil), request.Evidence...),
	}
}

type Request struct {
	RunID           string
	Goal            string
	Messages        []llm.Message
	AvailableTools  []tool.Spec
	PriorIterations []Iteration
	Evidence        []Evidence
	Budget          BudgetState
	Metadata        ExecutionMetadata
}

func (request Request) Validate() error {
	if strings.TrimSpace(request.RunID) == "" {
		return errors.New("Reactor request RunID is empty")
	}
	if strings.TrimSpace(request.Goal) == "" {
		return errors.New("Reactor request goal is empty")
	}
	if err := request.Budget.Validate(); err != nil {
		return err
	}
	return nil
}

type Result struct {
	FinalMessage *llm.Message
	Iterations   []Iteration
	Evidence     []Evidence
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
