package plan

import (
	"time"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type RunID string
type TaskID string
type EvidenceID string

type Goal struct {
	Objective          string      `json:"objective"`
	Constraints        []string    `json:"constraints,omitempty"`
	AcceptanceCriteria []Criterion `json:"acceptance_criteria,omitempty"`
}

type Criterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type ExecutionKind string

const (
	ExecutionPlanned ExecutionKind = "planned"
)

func (kind ExecutionKind) Valid() bool {
	switch kind {
	case ExecutionPlanned:
		return true
	default:
		return false
	}
}

type Budget struct {
	MaxIterations   int           `json:"max_iterations,omitempty"`
	MaxToolCalls    int           `json:"max_tool_calls,omitempty"`
	MaxInputTokens  int64         `json:"max_input_tokens,omitempty"`
	MaxOutputTokens int64         `json:"max_output_tokens,omitempty"`
	MaxDuration     time.Duration `json:"max_duration,omitempty"`
}

type BudgetState struct {
	Budget           Budget        `json:"budget"`
	IterationsUsed   int           `json:"iterations_used"`
	ToolCallsUsed    int           `json:"tool_calls_used"`
	InputTokensUsed  int64         `json:"input_tokens_used"`
	OutputTokensUsed int64         `json:"output_tokens_used"`
	Elapsed          time.Duration `json:"elapsed"`
}

type ExecutionGraph struct {
	Kind    ExecutionKind `json:"kind"`
	Version int           `json:"version"`
	Tasks   []Task        `json:"tasks"`
}

func NewPlanGraph() ExecutionGraph {
	return ExecutionGraph{Kind: ExecutionPlanned, Version: 1}
}

type Task struct {
	ID                 TaskID          `json:"id"`
	Objective          string          `json:"objective"`
	Dependencies       []TaskID        `json:"dependencies,omitempty"`
	AcceptanceCriteria []Criterion     `json:"acceptance_criteria,omitempty"`
	Status             TaskStatus      `json:"status"`
	Attempts           int             `json:"attempts"`
	Budget             Budget          `json:"budget,omitempty"`
	SideEffect         tool.SideEffect `json:"side_effect,omitempty"`
	Resources          []string        `json:"resources,omitempty"`
	Result             *TaskResult     `json:"result,omitempty"`
}

type TaskResult struct {
	Summary     string       `json:"summary"`
	EvidenceIDs []EvidenceID `json:"evidence_ids,omitempty"`
	Partial     bool         `json:"partial,omitempty"`
}

type EvidenceKind string

const (
	EvidenceTool       EvidenceKind = "tool"
	EvidenceFile       EvidenceKind = "file"
	EvidenceCommand    EvidenceKind = "command"
	EvidenceTest       EvidenceKind = "test"
	EvidenceDiagnostic EvidenceKind = "diagnostic"
)

type ArtifactRef struct {
	Path   string `json:"path,omitempty"`
	URI    string `json:"uri,omitempty"`
	Digest string `json:"digest,omitempty"`
}

type Evidence struct {
	ID           EvidenceID   `json:"id"`
	Kind         EvidenceKind `json:"kind"`
	Source       string       `json:"source"`
	Summary      string       `json:"summary"`
	CriterionIDs []string     `json:"criterion_ids,omitempty"`
	Artifact     *ArtifactRef `json:"artifact,omitempty"`
	Verified     bool         `json:"verified"`
}

type RunState struct {
	ID           RunID          `json:"id"`
	Goal         Goal           `json:"goal"`
	Graph        ExecutionGraph `json:"graph"`
	ActiveTaskID TaskID         `json:"active_task_id,omitempty"`
	Status       RunStatus      `json:"status"`
	Budget       BudgetState    `json:"budget"`
	Evidence     []Evidence     `json:"evidence,omitempty"`
	StopReason   StopReason     `json:"stop_reason,omitempty"`
}

func NewRun(id RunID, goal Goal, graph ExecutionGraph, budget Budget) RunState {
	return RunState{
		ID:     id,
		Goal:   goal,
		Graph:  graph,
		Status: RunStatusInitialized,
		Budget: BudgetState{Budget: budget},
	}
}
