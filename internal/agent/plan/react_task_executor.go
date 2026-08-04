package plan

import (
	"context"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/agent/react"
)

type Reactor interface {
	Run(context.Context, react.Request) (react.Result, error)
}

type ReActTaskExecutor struct {
	reactor Reactor
}

func NewReActTaskExecutor(reactor Reactor) (*ReActTaskExecutor, error) {
	if reactor == nil {
		return nil, fmt.Errorf("ReAct task executor Reactor is nil")
	}
	return &ReActTaskExecutor{reactor: reactor}, nil
}

func (executor *ReActTaskExecutor) Run(ctx context.Context, input TaskRunInput) (TaskOutcome, error) {
	if err := input.Validate(); err != nil {
		return TaskOutcome{}, err
	}
	result, err := executor.reactor.Run(event.WithTaskID(ctx, string(input.Task.ID)), react.Request{
		RunID: string(input.RunID), Goal: input.Task.Objective, Messages: input.Messages,
		AvailableTools: input.AvailableTools,
		Evidence:       evidenceToReact(input.Evidence), Budget: budgetToReact(input.Budget),
		Metadata: react.ExecutionMetadata{TaskID: string(input.Task.ID)},
	})
	if err != nil {
		return TaskOutcome{}, err
	}
	return taskOutcomeFromReact(result)
}

func taskOutcomeFromReact(result react.Result) (TaskOutcome, error) {
	outcome := TaskOutcome{
		Evidence: evidenceFromReact(result.Evidence),
		Budget:   budgetFromReact(result.Budget), Reason: result.Reason,
	}
	switch result.StopReason {
	case react.StopCompleted:
		outcome.Kind = TaskOutcomeCandidateComplete
		outcome.Candidate = &CandidateTaskResult{
			Result:       TaskResult{Summary: result.FinalMessage.Content, EvidenceIDs: verifiedEvidenceIDs(result.Evidence)},
			FinalMessage: *result.FinalMessage, Usage: result.Usage,
		}
	case react.StopBlocked:
		outcome.Kind = TaskOutcomeBlocked
		outcome.StopReason = StopReasonUserInputRequired
	case react.StopInterrupted:
		outcome.Kind = TaskOutcomeCancelled
		outcome.StopReason = StopReasonCancelled
	case react.StopBudgetExhausted:
		outcome.Kind = TaskOutcomeFailed
		outcome.StopReason = StopReasonBudgetExceeded
		if result.Limit != nil {
			limit := LimitReached{Limit: budgetLimitFromReact(result.Limit.Limit), Used: result.Limit.Used, Maximum: result.Limit.Maximum}
			if limit.Limit == BudgetLimitIterations {
				outcome.StopReason = StopReasonMaxIterations
			}
			outcome.Limit = &limit
		}
	case react.StopStalled, react.StopFailed:
		outcome.Kind = TaskOutcomeFailed
		outcome.StopReason = StopReasonToolError
	default:
		return TaskOutcome{}, fmt.Errorf("unsupported Reactor stop reason %q", result.StopReason)
	}
	return outcome, outcome.Validate()
}

func budgetToReact(state BudgetState) react.BudgetState {
	return react.BudgetState{Budget: react.Budget{
		MaxIterations: state.Budget.MaxIterations, MaxToolCalls: state.Budget.MaxToolCalls,
		MaxInputTokens: state.Budget.MaxInputTokens, MaxOutputTokens: state.Budget.MaxOutputTokens, MaxDuration: state.Budget.MaxDuration,
	}, IterationsUsed: state.IterationsUsed, ToolCallsUsed: state.ToolCallsUsed, InputTokensUsed: state.InputTokensUsed, OutputTokensUsed: state.OutputTokensUsed, Elapsed: state.Elapsed}
}

func budgetFromReact(state react.BudgetState) BudgetState {
	return BudgetState{Budget: Budget{
		MaxIterations: state.Budget.MaxIterations, MaxToolCalls: state.Budget.MaxToolCalls,
		MaxInputTokens: state.Budget.MaxInputTokens, MaxOutputTokens: state.Budget.MaxOutputTokens, MaxDuration: state.Budget.MaxDuration,
	}, IterationsUsed: state.IterationsUsed, ToolCallsUsed: state.ToolCallsUsed, InputTokensUsed: state.InputTokensUsed, OutputTokensUsed: state.OutputTokensUsed, Elapsed: state.Elapsed}
}

func evidenceToReact(values []Evidence) []react.Evidence {
	result := make([]react.Evidence, 0, len(values))
	for _, value := range values {
		var artifact *react.ArtifactRef
		if value.Artifact != nil {
			artifact = &react.ArtifactRef{Path: value.Artifact.Path, URI: value.Artifact.URI, Digest: value.Artifact.Digest}
		}
		result = append(result, react.Evidence{ID: react.EvidenceID(value.ID), Kind: react.EvidenceKind(value.Kind), Source: value.Source, Summary: value.Summary, CriterionIDs: append([]string(nil), value.CriterionIDs...), Artifact: artifact, Verified: value.Verified})
	}
	return result
}

func evidenceFromReact(values []react.Evidence) []Evidence {
	result := make([]Evidence, 0, len(values))
	for _, value := range values {
		var artifact *ArtifactRef
		if value.Artifact != nil {
			artifact = &ArtifactRef{Path: value.Artifact.Path, URI: value.Artifact.URI, Digest: value.Artifact.Digest}
		}
		result = append(result, Evidence{ID: EvidenceID(value.ID), Kind: EvidenceKind(value.Kind), Source: value.Source, Summary: value.Summary, CriterionIDs: append([]string(nil), value.CriterionIDs...), Artifact: artifact, Verified: value.Verified})
	}
	return result
}

func verifiedEvidenceIDs(values []react.Evidence) []EvidenceID {
	result := make([]EvidenceID, 0, len(values))
	for _, value := range values {
		if value.Verified {
			result = append(result, EvidenceID(value.ID))
		}
	}
	return result
}

func budgetLimitFromReact(limit react.LimitKind) BudgetLimit {
	switch limit {
	case react.LimitIterations:
		return BudgetLimitIterations
	case react.LimitToolCalls:
		return BudgetLimitToolCalls
	case react.LimitInputTokens:
		return BudgetLimitInputTokens
	case react.LimitOutputTokens:
		return BudgetLimitOutputTokens
	case react.LimitWallClock:
		return BudgetLimitWallClock
	default:
		return BudgetLimit("")
	}
}

var _ TaskRunner = (*ReActTaskExecutor)(nil)
