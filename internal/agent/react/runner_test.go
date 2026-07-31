package react

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type scriptedIterator struct {
	results []IterationResult
	inputs  []IterationInput
}

func (iterator *scriptedIterator) Run(_ context.Context, input IterationInput) (IterationResult, error) {
	iterator.inputs = append(iterator.inputs, input)
	if len(iterator.results) == 0 {
		return IterationResult{}, errors.New("unexpected model iteration")
	}
	result := iterator.results[0]
	iterator.results = iterator.results[1:]
	return result, nil
}

type scriptedCallExecutor struct {
	executions map[string]ToolExecution
	errors     map[string]error
	calls      []tool.Call
}

func (executor *scriptedCallExecutor) Execute(_ context.Context, call tool.Call) (ToolExecution, error) {
	executor.calls = append(executor.calls, call)
	return executor.executions[call.ID], executor.errors[call.ID]
}

type scriptedProgress struct {
	signals []ProgressSignal
	samples []ProgressSample
}

type blockingIterator struct {
	calls int
}

type cancellingCallExecutor struct {
	cancel context.CancelFunc
	calls  []tool.Call
}

func (executor *cancellingCallExecutor) Execute(_ context.Context, call tool.Call) (ToolExecution, error) {
	executor.calls = append(executor.calls, call)
	executor.cancel()
	execution := replayExecution(call.ID, call.Name, tool.Result{Text: "partial command output", Partial: true}, context.Canceled.Error())
	execution.Evidence.Kind = engine.EvidenceCommand
	execution.Evidence.Summary = "partial command output"
	return execution, context.Canceled
}

func (iterator *blockingIterator) Run(ctx context.Context, _ IterationInput) (IterationResult, error) {
	iterator.calls++
	<-ctx.Done()
	return IterationResult{}, ctx.Err()
}

func (progress *scriptedProgress) Observe(sample ProgressSample) ([]ProgressSignal, error) {
	progress.samples = append(progress.samples, sample)
	return append([]ProgressSignal(nil), progress.signals...), nil
}

func TestRunnerCompletesOneToolThenReturnsCandidate(t *testing.T) {
	call := tool.NewCall("call_1", "read_file", json.RawMessage(`{"path":"README.md"}`))
	iterator := &scriptedIterator{results: []IterationResult{
		{
			Kind: IterationToolCalls,
			Response: llm.Response{
				Message:      llm.AssistantToolCallMessage("", llm.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}),
				FinishReason: llm.FinishReasonToolCalls,
				Usage:        llm.Usage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12},
			},
			ToolCalls: []tool.Call{call},
		},
		{
			Kind: IterationCandidate,
			Response: llm.Response{
				Message: llm.AssistantMessage("repository inspected"), FinishReason: llm.FinishReasonStop,
				Usage: llm.Usage{InputTokens: 15, OutputTokens: 3, TotalTokens: 18},
			},
			Candidate: &engine.TaskResult{Summary: "repository inspected"},
		},
	}}
	execution := replayExecution("call_1", "read_file", tool.Result{Text: "README contents"}, "")
	execution.Evidence.Verified = true
	executor := &scriptedCallExecutor{executions: map[string]ToolExecution{"call_1": execution}, errors: map[string]error{}}
	progress := &scriptedProgress{}
	runner := newTestRunner(t, iterator, executor, progress)
	input := validRunnerInput()

	outcome, err := runner.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("run ReAct loop: %v", err)
	}
	if outcome.Kind != engine.TaskOutcomeCandidateComplete || outcome.Candidate == nil || outcome.Candidate.Result.Summary != "repository inspected" {
		t.Fatalf("unexpected candidate outcome: %#v", outcome)
	}
	if len(outcome.Steps) != 2 || len(outcome.Evidence) != 1 || len(executor.calls) != 1 || len(iterator.inputs) != 2 {
		t.Fatalf("unexpected loop state: outcome=%#v calls=%#v inputs=%#v", outcome, executor.calls, iterator.inputs)
	}
	if len(iterator.inputs[1].Messages) != 3 {
		t.Fatalf("second iteration did not receive assistant call and result: %#v", iterator.inputs[1].Messages)
	}
	if iterator.inputs[1].Messages[1].Role != llm.RoleAssistant || iterator.inputs[1].Messages[2].Role != llm.RoleTool || iterator.inputs[1].Messages[2].ToolCallID != "call_1" {
		t.Fatalf("unexpected replay history: %#v", iterator.inputs[1].Messages)
	}
	if outcome.Candidate.Usage.InputTokens != 25 || outcome.Candidate.Usage.OutputTokens != 5 || outcome.Candidate.Usage.TotalTokens != 30 {
		t.Fatalf("usage was not accumulated: %#v", outcome.Candidate.Usage)
	}
	if input.Task.Status != engine.TaskStatusRunning {
		t.Fatalf("runner mutated caller task status: %q", input.Task.Status)
	}
	if err := outcome.Validate(); err != nil {
		t.Fatalf("candidate outcome is invalid: %v", err)
	}
}

func TestRunnerReplaysToolFailureBeforeCandidate(t *testing.T) {
	call := tool.NewCall("call_failed", "read_file", json.RawMessage(`{"path":"missing"}`))
	iterator := &scriptedIterator{results: []IterationResult{
		{Kind: IterationToolCalls, Response: llm.Response{Message: llm.AssistantToolCallMessage("", llm.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}), FinishReason: llm.FinishReasonToolCalls}, ToolCalls: []tool.Call{call}},
		{Kind: IterationCandidate, Response: llm.Response{Message: llm.AssistantMessage("file was unavailable"), FinishReason: llm.FinishReasonStop}, Candidate: &engine.TaskResult{Summary: "file was unavailable"}},
	}}
	executionErr := errors.New("file not found")
	executor := &scriptedCallExecutor{
		executions: map[string]ToolExecution{"call_failed": replayExecution("call_failed", "read_file", tool.Result{}, executionErr.Error())},
		errors:     map[string]error{"call_failed": executionErr},
	}
	runner := newTestRunner(t, iterator, executor, &scriptedProgress{})

	outcome, err := runner.Run(context.Background(), validRunnerInput())
	if err != nil || outcome.Kind != engine.TaskOutcomeCandidateComplete {
		t.Fatalf("tool failure did not continue to candidate: outcome=%#v err=%v", outcome, err)
	}
	toolMessage := iterator.inputs[1].Messages[2]
	if !strings.Contains(toolMessage.Content, `"ok":false`) || !strings.Contains(toolMessage.Content, "file not found") {
		t.Fatalf("tool failure was not replayed: %#v", toolMessage)
	}
	if len(outcome.Evidence) != 1 || outcome.Evidence[0].Verified || len(outcome.Candidate.Result.EvidenceIDs) != 0 {
		t.Fatalf("failed evidence should remain in the run but not support the candidate: %#v", outcome)
	}
}

func TestRunnerCandidateReferencesOnlyVerifiedEvidenceAfterRecovery(t *testing.T) {
	failedCall := tool.NewCall("failed", "apply_patch", json.RawMessage(`{"patch":"conflict"}`))
	passedCall := tool.NewCall("passed", "execute_command", json.RawMessage(`{"command":"go test ./..."}`))
	iterator := &scriptedIterator{results: []IterationResult{
		{Kind: IterationToolCalls, Response: llm.Response{Message: llm.AssistantToolCallMessage("", llm.ToolCall{ID: failedCall.ID, Name: failedCall.Name, Arguments: failedCall.Arguments}), FinishReason: llm.FinishReasonToolCalls}, ToolCalls: []tool.Call{failedCall}},
		{Kind: IterationToolCalls, Response: llm.Response{Message: llm.AssistantToolCallMessage("", llm.ToolCall{ID: passedCall.ID, Name: passedCall.Name, Arguments: passedCall.Arguments}), FinishReason: llm.FinishReasonToolCalls}, ToolCalls: []tool.Call{passedCall}},
		{Kind: IterationCandidate, Response: llm.Response{Message: llm.AssistantMessage("recovered and verified"), FinishReason: llm.FinishReasonStop}, Candidate: &engine.TaskResult{Summary: "recovered and verified"}},
	}}
	failed := replayExecution("failed", "apply_patch", tool.Result{}, "patch conflict")
	passed := replayExecution("passed", "execute_command", tool.Result{Text: "tests passed"}, "")
	passed.Evidence.Verified = true
	executor := &scriptedCallExecutor{
		executions: map[string]ToolExecution{"failed": failed, "passed": passed},
		errors:     map[string]error{"failed": errors.New("patch conflict")},
	}
	runner := newTestRunner(t, iterator, executor, &scriptedProgress{})
	input := validRunnerInput()
	input.AvailableTools = []tool.Spec{
		{Name: "apply_patch", SideEffect: tool.SideEffectWrite, ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeExclusive}},
		{Name: "execute_command", SideEffect: tool.SideEffectExecute, ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeExclusive}},
	}

	outcome, err := runner.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("run recovered workflow: %v", err)
	}
	if len(outcome.Evidence) != 2 || len(outcome.Candidate.Result.EvidenceIDs) != 1 || outcome.Candidate.Result.EvidenceIDs[0] != passed.Evidence.ID {
		t.Fatalf("candidate evidence did not exclude recovered failure: %#v", outcome)
	}
}

func TestRunnerReturnsNeedsPlanFromProgressSignal(t *testing.T) {
	call := tool.NewCall("call_1", "read_file", json.RawMessage(`{"path":"README.md"}`))
	iterator := &scriptedIterator{results: []IterationResult{{
		Kind:      IterationToolCalls,
		Response:  llm.Response{Message: llm.AssistantToolCallMessage("", llm.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}), FinishReason: llm.FinishReasonToolCalls},
		ToolCalls: []tool.Call{call},
	}}}
	execution := replayExecution("call_1", "read_file", tool.Result{Text: "same result"}, "")
	executor := &scriptedCallExecutor{executions: map[string]ToolExecution{"call_1": execution}, errors: map[string]error{}}
	progress := &scriptedProgress{signals: []ProgressSignal{{Kind: ProgressRepeatedAction, Reason: "same call repeated", RecommendPlan: true}}}
	runner := newTestRunner(t, iterator, executor, progress)

	outcome, err := runner.Run(context.Background(), validRunnerInput())
	if err != nil {
		t.Fatalf("run needs-plan loop: %v", err)
	}
	if outcome.Kind != engine.TaskOutcomeNeedsPlan || outcome.Reason != "same call repeated" || len(outcome.Steps) != 1 {
		t.Fatalf("unexpected needs-plan outcome: %#v", outcome)
	}
	if len(iterator.inputs) != 1 {
		t.Fatalf("runner called model after needs-plan signal: %d", len(iterator.inputs))
	}
}

func TestRunnerDirectCandidateDoesNotCompleteTaskOrRun(t *testing.T) {
	iterator := &scriptedIterator{results: []IterationResult{{
		Kind:      IterationCandidate,
		Response:  llm.Response{Message: llm.AssistantMessage("candidate only"), FinishReason: llm.FinishReasonStop},
		Candidate: &engine.TaskResult{Summary: "candidate only"},
	}}}
	runner := newTestRunner(t, iterator, &scriptedCallExecutor{}, &scriptedProgress{})
	input := validRunnerInput()

	outcome, err := runner.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("run direct candidate: %v", err)
	}
	if outcome.Kind != engine.TaskOutcomeCandidateComplete || input.Task.Status != engine.TaskStatusRunning {
		t.Fatalf("candidate incorrectly completed task or run: outcome=%#v task=%#v", outcome, input.Task)
	}
}

func TestRunnerStopsBeforeModelWhenStepsExhausted(t *testing.T) {
	iterator := &scriptedIterator{}
	runner := newTestRunner(t, iterator, &scriptedCallExecutor{}, &scriptedProgress{})
	input := validRunnerInput()
	input.Task.Budget.MaxSteps = 2
	input.Budget.StepsUsed = 2

	outcome, err := runner.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("run exhausted budget: %v", err)
	}
	if len(iterator.inputs) != 0 || outcome.Kind != engine.TaskOutcomeFailed || outcome.StopReason != engine.StopReasonMaxSteps {
		t.Fatalf("unexpected exhausted outcome: %#v inputs=%d", outcome, len(iterator.inputs))
	}
	if outcome.Limit == nil || outcome.Limit.Limit != engine.BudgetLimitSteps || outcome.Limit.Used != 2 || outcome.Limit.Maximum != 2 {
		t.Fatalf("missing structured step limit: %#v", outcome.Limit)
	}
}

func TestRunnerExecutesToolThenStopsAtLastStep(t *testing.T) {
	call := tool.NewCall("call_1", "read_file", json.RawMessage(`{"path":"README.md"}`))
	iterator := &scriptedIterator{results: []IterationResult{
		{Kind: IterationToolCalls, Response: llm.Response{Message: llm.AssistantToolCallMessage("", llm.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}), FinishReason: llm.FinishReasonToolCalls}, ToolCalls: []tool.Call{call}},
		{Kind: IterationCandidate, Response: llm.Response{Message: llm.AssistantMessage("must not be called"), FinishReason: llm.FinishReasonStop}, Candidate: &engine.TaskResult{Summary: "must not be called"}},
	}}
	executor := &scriptedCallExecutor{executions: map[string]ToolExecution{"call_1": replayExecution("call_1", "read_file", tool.Result{Text: "ok"}, "")}, errors: map[string]error{}}
	runner := newTestRunner(t, iterator, executor, &scriptedProgress{})
	input := validRunnerInput()
	input.Budget.Budget.MaxSteps = 1

	outcome, err := runner.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("run last step: %v", err)
	}
	if len(iterator.inputs) != 1 || len(executor.calls) != 1 || outcome.StopReason != engine.StopReasonMaxSteps {
		t.Fatalf("runner crossed step boundary: outcome=%#v model=%d tools=%d", outcome, len(iterator.inputs), len(executor.calls))
	}
	if outcome.Budget.StepsUsed != 1 || outcome.Budget.ToolCallsUsed != 1 {
		t.Fatalf("unexpected consumed budget: %#v", outcome.Budget)
	}
}

func TestRunnerRejectsToolBatchBeyondBudget(t *testing.T) {
	calls := []tool.Call{
		tool.NewCall("call_1", "read_file", json.RawMessage(`{"path":"a"}`)),
		tool.NewCall("call_2", "read_file", json.RawMessage(`{"path":"b"}`)),
	}
	iterator := &scriptedIterator{results: []IterationResult{{
		Kind: IterationToolCalls,
		Response: llm.Response{Message: llm.AssistantToolCallMessage("",
			llm.ToolCall{ID: calls[0].ID, Name: calls[0].Name, Arguments: calls[0].Arguments},
			llm.ToolCall{ID: calls[1].ID, Name: calls[1].Name, Arguments: calls[1].Arguments}), FinishReason: llm.FinishReasonToolCalls},
		ToolCalls: calls,
	}}}
	executor := &scriptedCallExecutor{}
	runner := newTestRunner(t, iterator, executor, &scriptedProgress{})
	input := validRunnerInput()
	input.Budget.Budget.MaxToolCalls = 1

	outcome, err := runner.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("run tool budget: %v", err)
	}
	if len(iterator.inputs) != 1 || len(executor.calls) != 0 || outcome.StopReason != engine.StopReasonBudgetExceeded {
		t.Fatalf("tool batch was not rejected atomically: outcome=%#v model=%d tools=%d", outcome, len(iterator.inputs), len(executor.calls))
	}
	if outcome.Limit == nil || outcome.Limit.Limit != engine.BudgetLimitToolCalls || outcome.Limit.Used != 2 || outcome.Budget.ToolCallsUsed != 0 {
		t.Fatalf("unexpected tool limit detail: limit=%#v budget=%#v", outcome.Limit, outcome.Budget)
	}
}

func TestRunnerStopsBeforeToolsWhenModelExceedsTokenBudget(t *testing.T) {
	call := tool.NewCall("call_1", "read_file", json.RawMessage(`{"path":"README.md"}`))
	iterator := &scriptedIterator{results: []IterationResult{{
		Kind: IterationToolCalls,
		Response: llm.Response{
			Message:      llm.AssistantToolCallMessage("", llm.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}),
			FinishReason: llm.FinishReasonToolCalls,
			Usage:        llm.Usage{InputTokens: 11, OutputTokens: 1, TotalTokens: 12},
		},
		ToolCalls: []tool.Call{call},
	}}}
	executor := &scriptedCallExecutor{}
	runner := newTestRunner(t, iterator, executor, &scriptedProgress{})
	input := validRunnerInput()
	input.Budget.Budget.MaxInputTokens = 10

	outcome, err := runner.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("run token budget: %v", err)
	}
	if len(iterator.inputs) != 1 || len(executor.calls) != 0 || outcome.StopReason != engine.StopReasonBudgetExceeded {
		t.Fatalf("runner crossed token boundary: outcome=%#v model=%d tools=%d", outcome, len(iterator.inputs), len(executor.calls))
	}
	if outcome.Limit == nil || outcome.Limit.Limit != engine.BudgetLimitInputTokens || outcome.Budget.InputTokensUsed != 11 {
		t.Fatalf("unexpected token limit detail: limit=%#v budget=%#v", outcome.Limit, outcome.Budget)
	}
}

func TestRunnerReportsWallClockBudgetInsteadOfCancellation(t *testing.T) {
	iterator := &blockingIterator{}
	runner := newTestRunner(t, iterator, &scriptedCallExecutor{}, &scriptedProgress{})
	input := validRunnerInput()
	input.Budget.Budget.MaxDuration = 20 * time.Millisecond

	outcome, err := runner.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("run wall clock budget: %v", err)
	}
	if iterator.calls != 1 || outcome.Kind != engine.TaskOutcomeFailed || outcome.StopReason != engine.StopReasonBudgetExceeded {
		t.Fatalf("unexpected wall clock outcome: %#v calls=%d", outcome, iterator.calls)
	}
	if outcome.Limit == nil || outcome.Limit.Limit != engine.BudgetLimitWallClock {
		t.Fatalf("missing wall clock limit: %#v", outcome.Limit)
	}
}

func TestRunnerReturnsAccumulatedBudgetWithinLimits(t *testing.T) {
	iterator := &scriptedIterator{results: []IterationResult{{
		Kind:      IterationCandidate,
		Response:  llm.Response{Message: llm.AssistantMessage("done"), FinishReason: llm.FinishReasonStop, Usage: llm.Usage{InputTokens: 5, OutputTokens: 6, TotalTokens: 11}},
		Candidate: &engine.TaskResult{Summary: "done"},
	}}}
	runner := newTestRunner(t, iterator, &scriptedCallExecutor{}, &scriptedProgress{})
	input := validRunnerInput()
	input.Budget = engine.BudgetState{
		Budget:    engine.Budget{MaxSteps: 5, MaxToolCalls: 3, MaxInputTokens: 20, MaxOutputTokens: 10},
		StepsUsed: 2, ToolCallsUsed: 1, InputTokensUsed: 3, OutputTokensUsed: 4,
	}

	outcome, err := runner.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("run within budget: %v", err)
	}
	if outcome.Kind != engine.TaskOutcomeCandidateComplete || outcome.Budget.StepsUsed != 3 || outcome.Budget.ToolCallsUsed != 1 || outcome.Budget.InputTokensUsed != 8 || outcome.Budget.OutputTokensUsed != 10 {
		t.Fatalf("unexpected accumulated budget: %#v", outcome)
	}
	if len(iterator.inputs) != 1 || iterator.inputs[0].MaxOutputTokens != 6 {
		t.Fatalf("remaining output budget was not applied: %#v", iterator.inputs)
	}
}

func TestRunnerCancellationPreservesPartialToolStepAndEvidence(t *testing.T) {
	call := tool.NewCall("call_1", "execute_command", json.RawMessage(`{"command":"long-running"}`))
	iterator := &scriptedIterator{results: []IterationResult{{
		Kind:      IterationToolCalls,
		Response:  llm.Response{Message: llm.AssistantToolCallMessage("", llm.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}), FinishReason: llm.FinishReasonToolCalls},
		ToolCalls: []tool.Call{call},
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	executor := &cancellingCallExecutor{cancel: cancel}
	runner := newTestRunner(t, iterator, executor, &scriptedProgress{})
	input := validRunnerInput()
	input.AvailableTools[0].Name = "execute_command"

	outcome, err := runner.Run(ctx, input)
	if err != nil {
		t.Fatalf("run cancelled tool step: %v", err)
	}
	if outcome.Kind != engine.TaskOutcomeCancelled || len(outcome.Steps) != 1 || outcome.Steps[0].Status != engine.StepStatusCancelled || len(outcome.Evidence) != 1 {
		t.Fatalf("partial cancellation was not retained: %#v", outcome)
	}
	if !outcome.Steps[0].Observations[0].Result.Partial || outcome.Evidence[0].Summary == "" || outcome.Budget.ToolCallsUsed != 1 {
		t.Fatalf("partial result metadata was lost: %#v", outcome)
	}
}

func newTestRunner(t *testing.T, iterator ModelIterator, executor CallExecutor, progress ProgressObserver) *Runner {
	t.Helper()
	runner, err := NewRunner(iterator, executor, progress, RunnerOptions{Temperature: 0.2, MaxOutputTokens: 512})
	if err != nil {
		t.Fatalf("create ReAct runner: %v", err)
	}
	runner.now = fixedClock(
		time.Unix(0, 0), time.Unix(1, 0),
		time.Unix(2, 0), time.Unix(3, 0),
	)
	return runner
}

func validRunnerInput() engine.TaskRunInput {
	return engine.TaskRunInput{
		RunID:    "run_1",
		Task:     engine.Task{ID: "root", Objective: "inspect repository", Status: engine.TaskStatusRunning},
		Messages: []llm.Message{llm.UserMessage("inspect repository")},
		AvailableTools: []tool.Spec{{
			Name: "read_file", Description: "Read a file", InputSchema: json.RawMessage(`{"type":"object"}`),
			SideEffect: tool.SideEffectRead, ParallelSafe: true, Idempotent: true,
			ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"/path"}},
		}},
	}
}
