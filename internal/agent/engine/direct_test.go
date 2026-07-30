package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type scriptedTaskRunner struct {
	outcomes []TaskOutcome
	inputs   []TaskRunInput
}

func (runner *scriptedTaskRunner) Run(_ context.Context, input TaskRunInput) (TaskOutcome, error) {
	runner.inputs = append(runner.inputs, input)
	if len(runner.outcomes) == 0 {
		return TaskOutcome{}, errors.New("unexpected task runner call")
	}
	outcome := runner.outcomes[0]
	runner.outcomes = runner.outcomes[1:]
	return outcome, nil
}

type scriptedVerifier struct {
	results []Verification
	inputs  []VerificationInput
}

func (verifier *scriptedVerifier) Verify(_ context.Context, input VerificationInput) (Verification, error) {
	verifier.inputs = append(verifier.inputs, input)
	if len(verifier.results) == 0 {
		return Verification{}, errors.New("unexpected verifier call")
	}
	result := verifier.results[0]
	verifier.results = verifier.results[1:]
	return result, nil
}

type scriptedReflector struct {
	results []Reflection
	inputs  []ReflectionInput
}

func (reflector *scriptedReflector) Reflect(_ context.Context, input ReflectionInput) (Reflection, error) {
	reflector.inputs = append(reflector.inputs, input)
	if len(reflector.results) == 0 {
		return Reflection{}, errors.New("unexpected reflector call")
	}
	result := reflector.results[0]
	reflector.results = reflector.results[1:]
	return result, nil
}

func TestDirectEngineCompletesOnlyAfterVerificationAndReflection(t *testing.T) {
	evidence := Evidence{ID: "tests", Kind: EvidenceTest, Source: "go test", Summary: "passed", CriterionIDs: []string{"tests"}, Verified: true}
	runner := &scriptedTaskRunner{outcomes: []TaskOutcome{{
		Kind:      TaskOutcomeCandidateComplete,
		Candidate: &CandidateTaskResult{Result: TaskResult{Summary: "implemented", EvidenceIDs: []EvidenceID{"tests"}}, FinalMessage: llm.AssistantMessage("implemented")},
		Evidence:  []Evidence{evidence}, Budget: BudgetState{StepsUsed: 1},
	}}}
	verifier := NewDeterministicVerifier()
	reflector := &scriptedReflector{results: []Reflection{{Scope: ReflectionScopeTask, Verdict: ReflectionAccept, Lesson: "verify before completion"}}}
	direct := newTestDirectEngine(t, runner, verifier, reflector, 2)

	result, err := direct.Run(context.Background(), validDirectInput())
	if err != nil {
		t.Fatalf("run direct engine: %v", err)
	}
	root := result.State.Graph.Tasks[0]
	if result.State.Status != RunStatusCompleted || result.State.StopReason != StopReasonCompleted || root.Status != TaskStatusCompleted || root.Result == nil || root.Result.Summary != "implemented" {
		t.Fatalf("unexpected completed state: %#v", result.State)
	}
	if len(reflector.inputs) != 1 || reflector.inputs[0].Task.Status != TaskStatusReflecting || result.Verification == nil || !result.Verification.Passed() {
		t.Fatalf("quality gates were not applied: result=%#v reflector=%#v", result, reflector.inputs)
	}
}

func TestDirectEngineRetriesSameTaskWithFeedback(t *testing.T) {
	runner := &scriptedTaskRunner{outcomes: []TaskOutcome{
		{Kind: TaskOutcomeCandidateComplete, Candidate: candidate("first"), Budget: BudgetState{StepsUsed: 1}},
		{Kind: TaskOutcomeCandidateComplete, Candidate: candidate("second"), Budget: BudgetState{StepsUsed: 2}},
	}}
	verifier := &scriptedVerifier{results: []Verification{
		{Status: VerificationFailed, Checks: []VerificationCheck{{ID: "tests", Status: VerificationCheckFailed}}, EvidenceGaps: []string{"tests must pass"}},
		{Status: VerificationPassed},
	}}
	reflector := &scriptedReflector{results: []Reflection{
		{Scope: ReflectionScopeTask, Verdict: ReflectionRetry, EvidenceGaps: []string{"tests must pass"}, NextActionHint: "fix tests"},
		{Scope: ReflectionScopeTask, Verdict: ReflectionAccept},
	}}
	direct := newTestDirectEngine(t, runner, verifier, reflector, 2)

	result, err := direct.Run(context.Background(), validDirectInput())
	if err != nil {
		t.Fatalf("run retry flow: %v", err)
	}
	if result.State.Status != RunStatusCompleted || result.State.Graph.Tasks[0].Attempts != 2 || len(runner.inputs) != 2 || len(result.State.Reflections) != 2 {
		t.Fatalf("unexpected retry result: %#v inputs=%d", result.State, len(runner.inputs))
	}
	if len(runner.inputs[1].Messages) != 3 || !strings.Contains(runner.inputs[1].Messages[2].Content, "tests must pass") {
		t.Fatalf("retry feedback was not passed to runner: %#v", runner.inputs[1].Messages)
	}
}

func TestDirectEnginePausesForPlanning(t *testing.T) {
	runner := &scriptedTaskRunner{outcomes: []TaskOutcome{{Kind: TaskOutcomeNeedsPlan, Reason: "multiple dependent modules", Budget: BudgetState{StepsUsed: 1}}}}
	direct := newTestDirectEngine(t, runner, &scriptedVerifier{}, &scriptedReflector{}, 2)

	result, err := direct.Run(context.Background(), validDirectInput())
	if err != nil {
		t.Fatalf("run needs-plan flow: %v", err)
	}
	if result.State.Status != RunStatusPlanning || result.State.Graph.Tasks[0].Status != TaskStatusBlocked || result.Reason != "multiple dependent modules" {
		t.Fatalf("unexpected needs-plan state: %#v", result)
	}
}

func TestDirectEngineUsesReflectionReplanAndAskUser(t *testing.T) {
	tests := []struct {
		name       string
		reflection Reflection
		runStatus  RunStatus
		stopReason StopReason
	}{
		{name: "replan", reflection: Reflection{Scope: ReflectionScopeTask, Verdict: ReflectionReplan, Issues: []Issue{{Code: "scope", Summary: "multiple tasks required", Severity: IssueSeverityWarning}}, NextActionHint: "create a graph"}, runStatus: RunStatusPlanning},
		{name: "ask user", reflection: Reflection{Scope: ReflectionScopeTask, Verdict: ReflectionAskUser, NextActionHint: "choose target"}, runStatus: RunStatusSuspended, stopReason: StopReasonUserInputRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &scriptedTaskRunner{outcomes: []TaskOutcome{{Kind: TaskOutcomeCandidateComplete, Candidate: candidate("candidate")}}}
			verifier := &scriptedVerifier{results: []Verification{{Status: VerificationPassed}}}
			reflector := &scriptedReflector{results: []Reflection{test.reflection}}
			direct := newTestDirectEngine(t, runner, verifier, reflector, 2)

			result, err := direct.Run(context.Background(), validDirectInput())
			if err != nil || result.State.Status != test.runStatus || result.State.StopReason != test.stopReason || result.State.Graph.Tasks[0].Status != TaskStatusBlocked {
				t.Fatalf("unexpected pause result: result=%#v err=%v", result, err)
			}
		})
	}
}

func TestDirectEnginePropagatesStructuredRunnerFailure(t *testing.T) {
	runner := &scriptedTaskRunner{outcomes: []TaskOutcome{{
		Kind: TaskOutcomeFailed, StopReason: StopReasonBudgetExceeded, Reason: "token budget reached",
		Limit:  &LimitReached{Limit: BudgetLimitInputTokens, Used: 11, Maximum: 10},
		Budget: BudgetState{Budget: Budget{MaxInputTokens: 10}, InputTokensUsed: 11},
	}}}
	direct := newTestDirectEngine(t, runner, &scriptedVerifier{}, &scriptedReflector{}, 2)

	result, err := direct.Run(context.Background(), validDirectInput())
	if err != nil || result.State.Status != RunStatusFailed || result.State.StopReason != StopReasonBudgetExceeded || result.State.Budget.InputTokensUsed != 11 {
		t.Fatalf("unexpected failed result: result=%#v err=%v", result, err)
	}
}

func TestDirectEngineStopsAfterRetryLimit(t *testing.T) {
	runner := &scriptedTaskRunner{outcomes: []TaskOutcome{{Kind: TaskOutcomeCandidateComplete, Candidate: candidate("first")}}}
	verifier := &scriptedVerifier{results: []Verification{{Status: VerificationFailed, EvidenceGaps: []string{"tests"}}}}
	reflector := &scriptedReflector{results: []Reflection{{Scope: ReflectionScopeTask, Verdict: ReflectionRetry, EvidenceGaps: []string{"tests"}}}}
	direct := newTestDirectEngine(t, runner, verifier, reflector, 1)

	result, err := direct.Run(context.Background(), validDirectInput())
	if err != nil || result.State.Status != RunStatusFailed || result.State.StopReason != StopReasonVerificationFailed || len(runner.inputs) != 1 {
		t.Fatalf("unexpected exhausted retry result: result=%#v err=%v", result, err)
	}
}

func TestDirectEnginePublishesQualityGateAndTerminalEvents(t *testing.T) {
	evidence := Evidence{ID: "tests", Kind: EvidenceTest, Source: "go test", Summary: "passed", CriterionIDs: []string{"tests"}, Verified: true}
	runner := &scriptedTaskRunner{outcomes: []TaskOutcome{{
		Kind: TaskOutcomeCandidateComplete, Candidate: &CandidateTaskResult{Result: TaskResult{Summary: "done", EvidenceIDs: []EvidenceID{"tests"}}, FinalMessage: llm.AssistantMessage("done")},
		Evidence: []Evidence{evidence},
	}}}
	sink := event.NewMemorySink()
	direct, err := NewDirectEngine(runner, NewDeterministicVerifier(), &scriptedReflector{results: []Reflection{{Scope: ReflectionScopeTask, Verdict: ReflectionAccept}}}, sink, DirectEngineOptions{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("create direct engine: %v", err)
	}

	result, err := direct.Run(context.Background(), validDirectInput())
	if err != nil || result.State.Status != RunStatusCompleted {
		t.Fatalf("run direct engine: result=%#v err=%v", result, err)
	}
	events := sink.Snapshot()
	if len(events) < 6 || events[0].Type() != event.TypeEngineRunStarted || events[len(events)-1].Type() != event.TypeEngineRunCompleted {
		t.Fatalf("unexpected engine event envelope: %#v", events)
	}
	var verificationSeen, reflectionSeen bool
	for _, runtimeEvent := range events {
		switch runtimeEvent.Type() {
		case event.TypeVerificationDone:
			verificationSeen = true
		case event.TypeReflectionDone:
			reflectionSeen = true
		}
	}
	if !verificationSeen || !reflectionSeen {
		t.Fatalf("quality gate events missing: %#v", events)
	}
}

func TestDirectEngineCancellationKeepsPartialStepAndEvidence(t *testing.T) {
	partialEvidence := Evidence{ID: "partial", Kind: EvidenceCommand, Source: "execute_command", Summary: "partial output"}
	partialStep := Step{Index: 0, Status: StepStatusCancelled, Evidence: []Evidence{partialEvidence}}
	runner := &scriptedTaskRunner{outcomes: []TaskOutcome{{
		Kind: TaskOutcomeCancelled, StopReason: StopReasonCancelled, Reason: "context canceled",
		Steps: []Step{partialStep}, Evidence: []Evidence{partialEvidence}, Budget: BudgetState{StepsUsed: 1, ToolCallsUsed: 1},
	}}}
	sink := event.NewMemorySink()
	direct, err := NewDirectEngine(runner, &scriptedVerifier{}, &scriptedReflector{}, sink, DirectEngineOptions{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("create direct engine: %v", err)
	}

	result, err := direct.Run(context.Background(), validDirectInput())
	if err != nil {
		t.Fatalf("run cancelled engine: %v", err)
	}
	if result.State.Status != RunStatusCancelled || result.State.StopReason != StopReasonCancelled || len(result.Steps) != 1 || result.Steps[0].Status != StepStatusCancelled || len(result.State.Evidence) != 1 {
		t.Fatalf("partial cancellation was not preserved: %#v", result)
	}
	events := sink.Snapshot()
	terminal, ok := events[len(events)-1].(event.EngineRunCompleted)
	if !ok || terminal.Status != string(RunStatusCancelled) || terminal.StopReason != string(StopReasonCancelled) {
		t.Fatalf("missing cancellation terminal event: %#v", events)
	}
}

func TestDirectEngineCancelledContextStillEmitsLifecycle(t *testing.T) {
	runner := &scriptedTaskRunner{}
	sink := event.NewMemorySink()
	direct, err := NewDirectEngine(runner, &scriptedVerifier{}, &scriptedReflector{}, sink, DirectEngineOptions{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("create direct engine: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := direct.Run(ctx, validDirectInput())
	if err != nil || result.State.Status != RunStatusCancelled || len(runner.inputs) != 0 {
		t.Fatalf("unexpected pre-cancelled result: result=%#v err=%v", result, err)
	}
	events := sink.Snapshot()
	if len(events) != 4 || events[0].Type() != event.TypeEngineRunStarted || events[len(events)-1].Type() != event.TypeEngineRunCompleted {
		t.Fatalf("unexpected cancellation events: %#v", events)
	}
}

func newTestDirectEngine(t *testing.T, runner TaskRunner, verifier Verifier, reflector Reflector, maxAttempts int) *DirectEngine {
	t.Helper()
	direct, err := NewDirectEngine(runner, verifier, reflector, event.NewMemorySink(), DirectEngineOptions{MaxAttempts: maxAttempts})
	if err != nil {
		t.Fatalf("create direct engine: %v", err)
	}
	return direct
}

func validDirectInput() DirectRunInput {
	goal := Goal{Objective: "implement feature", AcceptanceCriteria: []Criterion{{ID: "tests", Description: "tests pass", Required: true}}}
	return DirectRunInput{
		State:    NewRun("run_1", goal, NewDirectGraph(goal), Budget{MaxSteps: 10}),
		Messages: []llm.Message{llm.UserMessage("implement feature")},
	}
}

func candidate(summary string) *CandidateTaskResult {
	return &CandidateTaskResult{Result: TaskResult{Summary: summary}, FinalMessage: llm.AssistantMessage(summary)}
}
