package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type TaskOutcomeKind string

const (
	TaskOutcomeCandidateComplete TaskOutcomeKind = "candidate_complete"
	TaskOutcomeNeedsPlan         TaskOutcomeKind = "needs_plan"
	TaskOutcomeBlocked           TaskOutcomeKind = "blocked"
	TaskOutcomeFailed            TaskOutcomeKind = "failed"
	TaskOutcomeCancelled         TaskOutcomeKind = "cancelled"
)

func (kind TaskOutcomeKind) Valid() bool {
	switch kind {
	case TaskOutcomeCandidateComplete, TaskOutcomeNeedsPlan, TaskOutcomeBlocked, TaskOutcomeFailed, TaskOutcomeCancelled:
		return true
	default:
		return false
	}
}

type BudgetLimit string

const (
	BudgetLimitSteps        BudgetLimit = "steps"
	BudgetLimitToolCalls    BudgetLimit = "tool_calls"
	BudgetLimitInputTokens  BudgetLimit = "input_tokens"
	BudgetLimitOutputTokens BudgetLimit = "output_tokens"
	BudgetLimitWallClock    BudgetLimit = "wall_clock"
)

func (limit BudgetLimit) Valid() bool {
	switch limit {
	case BudgetLimitSteps, BudgetLimitToolCalls, BudgetLimitInputTokens, BudgetLimitOutputTokens, BudgetLimitWallClock:
		return true
	default:
		return false
	}
}

type LimitReached struct {
	Limit   BudgetLimit `json:"limit"`
	Used    int64       `json:"used"`
	Maximum int64       `json:"maximum"`
}

func (limit LimitReached) Validate() error {
	if !limit.Limit.Valid() {
		return fmt.Errorf("budget limit %q is invalid", limit.Limit)
	}
	if limit.Used < 0 || limit.Maximum <= 0 {
		return errors.New("budget limit usage must be non-negative with a positive maximum")
	}
	return nil
}

type TaskRunInput struct {
	RunID          RunID         `json:"run_id"`
	Task           Task          `json:"task"`
	Messages       []llm.Message `json:"messages,omitempty"`
	AvailableTools []tool.Spec   `json:"available_tools,omitempty"`
	PriorSteps     []Step        `json:"prior_steps,omitempty"`
	Evidence       []Evidence    `json:"evidence,omitempty"`
	Budget         BudgetState   `json:"budget"`
}

func (input TaskRunInput) Validate() error {
	if strings.TrimSpace(string(input.RunID)) == "" {
		return errors.New("task run input run ID is empty")
	}
	if strings.TrimSpace(string(input.Task.ID)) == "" {
		return errors.New("task run input task ID is empty")
	}
	if strings.TrimSpace(input.Task.Objective) == "" {
		return errors.New("task run input objective is empty")
	}
	if input.Task.Status != TaskStatusRunning {
		return fmt.Errorf("task run input task status must be %q", TaskStatusRunning)
	}
	if err := input.Budget.Validate(); err != nil {
		return fmt.Errorf("task run input budget: %w", err)
	}
	if err := (BudgetState{Budget: input.Task.Budget}).Validate(); err != nil {
		return fmt.Errorf("task budget: %w", err)
	}
	return nil
}

func (state BudgetState) Validate() error {
	if state.Budget.MaxSteps < 0 || state.Budget.MaxToolCalls < 0 || state.Budget.MaxInputTokens < 0 || state.Budget.MaxOutputTokens < 0 || state.Budget.MaxDuration < 0 {
		return errors.New("limits cannot be negative")
	}
	if state.StepsUsed < 0 || state.ToolCallsUsed < 0 || state.InputTokensUsed < 0 || state.OutputTokensUsed < 0 || state.Elapsed < 0 {
		return errors.New("usage cannot be negative")
	}
	return nil
}

type TaskOutcome struct {
	Kind       TaskOutcomeKind      `json:"kind"`
	Candidate  *CandidateTaskResult `json:"candidate,omitempty"`
	Steps      []Step               `json:"steps,omitempty"`
	Evidence   []Evidence           `json:"evidence,omitempty"`
	Budget     BudgetState          `json:"budget"`
	StopReason StopReason           `json:"stop_reason,omitempty"`
	Limit      *LimitReached        `json:"limit,omitempty"`
	Reason     string               `json:"reason,omitempty"`
}

type CandidateTaskResult struct {
	Result       TaskResult  `json:"result"`
	FinalMessage llm.Message `json:"final_message"`
	Usage        llm.Usage   `json:"usage"`
}

func (outcome TaskOutcome) Validate() error {
	if !outcome.Kind.Valid() {
		return fmt.Errorf("task outcome kind %q is invalid", outcome.Kind)
	}
	switch outcome.Kind {
	case TaskOutcomeCandidateComplete:
		if outcome.Candidate == nil {
			return errors.New("candidate_complete task outcome requires a candidate result")
		}
		if outcome.StopReason != "" || outcome.Limit != nil {
			return errors.New("candidate_complete task outcome cannot have a stop reason or budget limit")
		}
	case TaskOutcomeNeedsPlan:
		if strings.TrimSpace(outcome.Reason) == "" {
			return errors.New("needs_plan task outcome requires a reason")
		}
		if outcome.Candidate != nil || outcome.StopReason != "" || outcome.Limit != nil {
			return errors.New("needs_plan task outcome cannot have a candidate, stop reason, or budget limit")
		}
	case TaskOutcomeBlocked:
		if strings.TrimSpace(outcome.Reason) == "" {
			return errors.New("blocked task outcome requires a reason")
		}
		if outcome.Candidate != nil || outcome.StopReason != StopReasonUserInputRequired || outcome.Limit != nil {
			return fmt.Errorf("blocked task outcome must use stop reason %q", StopReasonUserInputRequired)
		}
	case TaskOutcomeFailed:
		if !outcome.StopReason.Valid() || outcome.StopReason == StopReasonCompleted || outcome.StopReason == StopReasonCancelled || outcome.StopReason == StopReasonUserInputRequired {
			return errors.New("failed task outcome requires a failure stop reason")
		}
		if outcome.Candidate != nil {
			return errors.New("failed task outcome cannot have a candidate")
		}
		if outcome.StopReason == StopReasonMaxSteps {
			if outcome.Limit == nil || outcome.Limit.Limit != BudgetLimitSteps {
				return errors.New("max_steps task outcome requires steps limit detail")
			}
		}
		if outcome.StopReason == StopReasonBudgetExceeded && outcome.Limit == nil {
			return errors.New("budget_exceeded task outcome requires limit detail")
		}
		if outcome.Limit != nil {
			if err := outcome.Limit.Validate(); err != nil {
				return err
			}
			if outcome.StopReason != StopReasonMaxSteps && outcome.StopReason != StopReasonBudgetExceeded {
				return errors.New("only budget failures can include limit detail")
			}
		}
	case TaskOutcomeCancelled:
		if outcome.Candidate != nil || outcome.StopReason != StopReasonCancelled || outcome.Limit != nil {
			return fmt.Errorf("cancelled task outcome must use stop reason %q", StopReasonCancelled)
		}
	}
	if err := outcome.Budget.Validate(); err != nil {
		return fmt.Errorf("task outcome budget: %w", err)
	}
	return nil
}

type ReActRunner interface {
	Run(context.Context, TaskRunInput) (TaskOutcome, error)
}

type TaskRunner = ReActRunner
