package engine

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
	ExecutionDirect    ExecutionKind = "direct"
	ExecutionPlanned   ExecutionKind = "planned"
	ExecutionDelegated ExecutionKind = "delegated"
)

func (kind ExecutionKind) Valid() bool {
	switch kind {
	case ExecutionDirect, ExecutionPlanned, ExecutionDelegated:
		return true
	default:
		return false
	}
}

type Budget struct {
	MaxSteps        int           `json:"max_steps,omitempty"`
	MaxToolCalls    int           `json:"max_tool_calls,omitempty"`
	MaxInputTokens  int64         `json:"max_input_tokens,omitempty"`
	MaxOutputTokens int64         `json:"max_output_tokens,omitempty"`
	MaxDuration     time.Duration `json:"max_duration,omitempty"`
}

type BudgetState struct {
	Budget           Budget        `json:"budget"`
	StepsUsed        int           `json:"steps_used"`
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

func NewDirectGraph(goal Goal) ExecutionGraph {
	return ExecutionGraph{
		Kind:    ExecutionDirect,
		Version: 1,
		Tasks: []Task{{
			ID:                 TaskID("root"),
			Objective:          goal.Objective,
			AcceptanceCriteria: append([]Criterion(nil), goal.AcceptanceCriteria...),
			Status:             TaskStatusPending,
		}},
	}
}

type Task struct {
	ID                 TaskID      `json:"id"`
	Objective          string      `json:"objective"`
	Dependencies       []TaskID    `json:"dependencies,omitempty"`
	AcceptanceCriteria []Criterion `json:"acceptance_criteria,omitempty"`
	Status             TaskStatus  `json:"status"`
	Attempts           int         `json:"attempts"`
	Budget             Budget      `json:"budget,omitempty"`
	Result             *TaskResult `json:"result,omitempty"`
}

type TaskResult struct {
	Summary     string       `json:"summary"`
	EvidenceIDs []EvidenceID `json:"evidence_ids,omitempty"`
	Partial     bool         `json:"partial,omitempty"`
}

type DecisionSummary struct {
	Intent     string `json:"intent,omitempty"`
	NextAction string `json:"next_action,omitempty"`
}

type StepStatus string

const (
	StepStatusRunning   StepStatus = "running"
	StepStatusCompleted StepStatus = "completed"
	StepStatusFailed    StepStatus = "failed"
	StepStatusCancelled StepStatus = "cancelled"
)

type Step struct {
	Index        int             `json:"index"`
	Decision     DecisionSummary `json:"decision"`
	ToolCalls    []tool.Call     `json:"tool_calls,omitempty"`
	Observations []Observation   `json:"observations,omitempty"`
	Evidence     []Evidence      `json:"evidence,omitempty"`
	Status       StepStatus      `json:"status"`
	StartedAt    time.Time       `json:"started_at"`
	CompletedAt  *time.Time      `json:"completed_at,omitempty"`
}

type Observation struct {
	CallID   string        `json:"call_id"`
	ToolName string        `json:"tool_name"`
	Result   tool.Result   `json:"result"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
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
	Reflections  []Reflection   `json:"reflections,omitempty"`
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
