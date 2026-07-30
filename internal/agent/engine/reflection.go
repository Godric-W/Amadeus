package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type ReflectionScope string

const (
	ReflectionScopeTask ReflectionScope = "task"
	ReflectionScopeRun  ReflectionScope = "run"
)

func (scope ReflectionScope) Valid() bool {
	return scope == ReflectionScopeTask || scope == ReflectionScopeRun
}

type ReflectionVerdict string

const (
	ReflectionAccept  ReflectionVerdict = "accept"
	ReflectionRetry   ReflectionVerdict = "retry"
	ReflectionReplan  ReflectionVerdict = "replan"
	ReflectionAskUser ReflectionVerdict = "ask_user"
	ReflectionAbort   ReflectionVerdict = "abort"
)

func (verdict ReflectionVerdict) Valid() bool {
	switch verdict {
	case ReflectionAccept, ReflectionRetry, ReflectionReplan, ReflectionAskUser, ReflectionAbort:
		return true
	default:
		return false
	}
}

type IssueSeverity string

const (
	IssueSeverityInfo     IssueSeverity = "info"
	IssueSeverityWarning  IssueSeverity = "warning"
	IssueSeverityCritical IssueSeverity = "critical"
)

func (severity IssueSeverity) Valid() bool {
	switch severity {
	case IssueSeverityInfo, IssueSeverityWarning, IssueSeverityCritical:
		return true
	default:
		return false
	}
}

type Issue struct {
	Code     string        `json:"code"`
	Summary  string        `json:"summary"`
	Severity IssueSeverity `json:"severity"`
}

type PlanChange struct {
	TaskID      TaskID `json:"task_id,omitempty"`
	Description string `json:"description"`
}

type ReflectionInput struct {
	RunID        RunID               `json:"run_id"`
	Task         Task                `json:"task"`
	Candidate    CandidateTaskResult `json:"candidate"`
	Verification Verification        `json:"verification"`
	Attempts     int                 `json:"attempts"`
	Budget       BudgetState         `json:"budget"`
}

func (input ReflectionInput) Validate() error {
	if strings.TrimSpace(string(input.RunID)) == "" {
		return errors.New("reflection input run ID is empty")
	}
	if strings.TrimSpace(string(input.Task.ID)) == "" {
		return errors.New("reflection input task ID is empty")
	}
	if input.Task.Status != TaskStatusReflecting {
		return fmt.Errorf("reflection input task status must be %q", TaskStatusReflecting)
	}
	if strings.TrimSpace(input.Candidate.Result.Summary) == "" {
		return errors.New("reflection input candidate summary is empty")
	}
	if err := input.Verification.Validate(); err != nil {
		return fmt.Errorf("reflection input verification: %w", err)
	}
	if input.Attempts < 0 {
		return errors.New("reflection input attempts cannot be negative")
	}
	if err := input.Budget.Validate(); err != nil {
		return fmt.Errorf("reflection input budget: %w", err)
	}
	return nil
}

type Reflection struct {
	Scope          ReflectionScope   `json:"scope"`
	Verdict        ReflectionVerdict `json:"verdict"`
	Issues         []Issue           `json:"issues,omitempty"`
	EvidenceGaps   []string          `json:"evidence_gaps,omitempty"`
	NextActionHint string            `json:"next_action_hint,omitempty"`
	PlanChanges    []PlanChange      `json:"plan_changes,omitempty"`
	Lesson         string            `json:"lesson,omitempty"`
}

func (reflection Reflection) Validate(verification Verification) error {
	if !reflection.Scope.Valid() {
		return fmt.Errorf("reflection scope %q is invalid", reflection.Scope)
	}
	if !reflection.Verdict.Valid() {
		return fmt.Errorf("reflection verdict %q is invalid", reflection.Verdict)
	}
	for index, issue := range reflection.Issues {
		if strings.TrimSpace(issue.Code) == "" || strings.TrimSpace(issue.Summary) == "" {
			return fmt.Errorf("reflection issue %d requires code and summary", index)
		}
		if !issue.Severity.Valid() {
			return fmt.Errorf("reflection issue %q severity %q is invalid", issue.Code, issue.Severity)
		}
	}
	for index, change := range reflection.PlanChanges {
		if strings.TrimSpace(change.Description) == "" {
			return fmt.Errorf("reflection plan change %d description is empty", index)
		}
	}
	if reflection.Verdict == ReflectionAccept {
		if !verification.Passed() {
			return errors.New("reflection cannot accept a failed verification")
		}
		if len(reflection.Issues) != 0 || len(reflection.EvidenceGaps) != 0 || len(reflection.PlanChanges) != 0 {
			return errors.New("accepted reflection cannot contain issues, evidence gaps, or plan changes")
		}
	}
	if reflection.Verdict != ReflectionAccept && len(reflection.Issues) == 0 && len(reflection.EvidenceGaps) == 0 && strings.TrimSpace(reflection.NextActionHint) == "" {
		return errors.New("non-accept reflection requires an issue, evidence gap, or next action hint")
	}
	if reflection.Verdict != ReflectionReplan && len(reflection.PlanChanges) != 0 {
		return errors.New("only replan reflection can contain plan changes")
	}
	return nil
}

type Reflector interface {
	Reflect(context.Context, ReflectionInput) (Reflection, error)
}
