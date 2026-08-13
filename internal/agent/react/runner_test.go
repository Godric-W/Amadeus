package react

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type scriptedIterator struct {
	results []IterationResult
	errors  []error
	inputs  []IterationInput
}

func (iterator *scriptedIterator) Run(_ context.Context, input IterationInput) (IterationResult, error) {
	iterator.inputs = append(iterator.inputs, input)
	index := len(iterator.inputs) - 1
	if index < len(iterator.errors) && iterator.errors[index] != nil {
		return IterationResult{}, iterator.errors[index]
	}
	if index >= len(iterator.results) {
		return IterationResult{}, errors.New("unexpected model iteration")
	}
	return iterator.results[index], nil
}

type scriptedCallExecutor struct {
	executions map[string]ToolOutcome
	errors     map[string]error
	canonical  map[string]tool.ToolCall
	calls      []tool.ToolCall
}

func (executor *scriptedCallExecutor) ExecuteBatch(ctx context.Context, calls []tool.ToolCall, recorder tool.NormalizedCallRecorder) ([]tool.ToolExecution, error) {
	executions := make([]tool.ToolExecution, 0, len(calls))
	for _, call := range calls {
		executor.calls = append(executor.calls, call)
		if err := executor.errors[call.ID]; err != nil {
			return nil, err
		}
		canonical := call
		if replacement, ok := executor.canonical[call.ID]; ok {
			canonical = replacement
		}
		executions = append(executions, executionFromOutcome(canonical, executor.executions[call.ID]))
	}
	if recorder != nil {
		canonical := make([]tool.ToolCall, len(executions))
		for index, execution := range executions {
			canonical[index] = execution.Call
		}
		if err := recorder(ctx, canonical); err != nil {
			return nil, err
		}
	}
	return executions, nil
}

type scriptedProgress struct {
	signals []ProgressSignal
	samples []ProgressSample
}

type orderingRolloutRecorder struct {
	mutex       sync.Mutex
	callsSaved  bool
	executed    []string
	outcomeIDs  []string
	callMessage llm.Message
}

func (recorder *orderingRolloutRecorder) RecordToolCalls(_ context.Context, message llm.Message) error {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	recorder.callsSaved = true
	recorder.callMessage = message
	return nil
}

func (recorder *orderingRolloutRecorder) RecordToolOutcomes(_ context.Context, outcomes []ToolOutcome) error {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	for _, outcome := range outcomes {
		recorder.outcomeIDs = append(recorder.outcomeIDs, outcome.CallID)
	}
	return nil
}

type orderingExecutor struct {
	recorder *orderingRolloutRecorder
}

func (executor orderingExecutor) ExecuteBatch(ctx context.Context, calls []tool.ToolCall, recorder tool.NormalizedCallRecorder) ([]tool.ToolExecution, error) {
	if recorder != nil {
		if err := recorder(ctx, calls); err != nil {
			return nil, err
		}
	}
	executions := make([]tool.ToolExecution, 0, len(calls))
	for _, call := range calls {
		executor.recorder.mutex.Lock()
		if !executor.recorder.callsSaved {
			executor.recorder.mutex.Unlock()
			return nil, errors.New("Tool Call executed before rollout persistence")
		}
		executor.recorder.executed = append(executor.recorder.executed, call.ID)
		executor.recorder.mutex.Unlock()
		if call.ID == "first" {
			time.Sleep(20 * time.Millisecond)
		}
		executions = append(executions, executionFromOutcome(call, successfulExecution(call, call.ID)))
	}
	return executions, nil
}

func (progress *scriptedProgress) Observe(sample ProgressSample) ([]ProgressSignal, error) {
	progress.samples = append(progress.samples, sample)
	return append([]ProgressSignal(nil), progress.signals...), nil
}

func TestRunnerCompletesFromFinalModelMessage(t *testing.T) {
	iterator := &scriptedIterator{results: []IterationResult{candidateIteration("done")}}
	runner := newTestRunner(t, iterator, &scriptedCallExecutor{executions: map[string]ToolOutcome{}, errors: map[string]error{}}, &scriptedProgress{})
	result, err := runner.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != StopCompleted || result.FinalMessage == nil || result.FinalMessage.Content != "done" || len(result.Iterations) != 1 || result.Budget.IterationsUsed != 1 {
		t.Fatalf("unexpected completed result: %#v", result)
	}
}

func TestRunnerExecutesToolsReplaysResultsAndCompletes(t *testing.T) {
	call := tool.NewCall("call-1", "read", []byte(`{"path":"README.md"}`))
	iterator := &scriptedIterator{results: []IterationResult{toolIteration(call), candidateIteration("repository inspected")}}
	execution := successfulExecution(call, "contents")
	executor := &scriptedCallExecutor{executions: map[string]ToolOutcome{call.ID: execution}, errors: map[string]error{}}
	progress := &scriptedProgress{}
	runner := newTestRunner(t, iterator, executor, progress)
	result, err := runner.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != StopCompleted || len(result.Iterations) != 2 || len(result.Iterations[0].Outcomes) != 1 || result.Budget.ToolCallsUsed != 1 {
		t.Fatalf("unexpected tool result: %#v", result)
	}
	if len(iterator.inputs) != 2 || len(iterator.inputs[1].Messages) != 3 || iterator.inputs[1].Messages[2].Role != llm.RoleTool {
		t.Fatalf("tool result was not replayed: %#v", iterator.inputs)
	}
}

func TestRunnerBuildsFreshPromptFromContextBeforeEveryThink(t *testing.T) {
	call := tool.NewCall("call-1", "read", []byte(`{"path":"README.md"}`))
	iterator := &scriptedIterator{results: []IterationResult{toolIteration(call), candidateIteration("done")}}
	executor := &scriptedCallExecutor{executions: map[string]ToolOutcome{call.ID: successfulExecution(call, "contents")}, errors: map[string]error{}}
	runner := newTestRunner(t, iterator, executor, &scriptedProgress{})
	request := validRequest()
	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != StopCompleted {
		t.Fatalf("unexpected dynamic request result: %#v", result)
	}
	if len(iterator.inputs) != 2 || len(iterator.inputs[1].Messages) != 3 || iterator.inputs[1].Messages[2].Role != llm.RoleTool {
		t.Fatalf("fresh ContextManager snapshot did not drive the next Think: %#v", iterator.inputs)
	}
	if len(executor.calls) != 1 || executor.calls[0].Name != "read" {
		t.Fatalf("Act did not use the Tool snapshot from the sampled request view: %#v", executor.calls)
	}
}

func TestRunnerInvokesBeforeSampleAndUsesReplacedContextEveryIteration(t *testing.T) {
	call := tool.NewCall("call-1", "read", []byte(`{"path":"README.md"}`))
	iterator := &scriptedIterator{results: []IterationResult{toolIteration(call), candidateIteration("done")}}
	runner := newTestRunner(t, iterator, &scriptedCallExecutor{executions: map[string]ToolOutcome{call.ID: successfulExecution(call, "contents")}, errors: map[string]error{}}, &scriptedProgress{})
	request := validRequest()
	samples := 0
	request.BeforeSample = func(_ context.Context, manager *agentcontext.Manager) error {
		samples++
		if samples == 1 {
			manager.Replace(llm.UserMessage("compacted objective"))
		}
		return nil
	}
	result, err := runner.Run(context.Background(), request)
	if err != nil || result.StopReason != StopCompleted {
		t.Fatalf("run result=%#v err=%v", result, err)
	}
	if samples != 2 {
		t.Fatalf("BeforeSample invocation count = %d, want 2", samples)
	}
	if iterator.inputs[0].Messages[0].Content != "compacted objective" || len(iterator.inputs[1].Messages) != 3 || iterator.inputs[1].Messages[0].Content != "compacted objective" {
		t.Fatalf("fresh replaced Context was not sampled: %#v", iterator.inputs)
	}
}

func TestRunnerRejectsPromptThatStillExceedsContextWindow(t *testing.T) {
	iterator := &scriptedIterator{results: []IterationResult{candidateIteration("must not run")}}
	runner := newTestRunner(t, iterator, &scriptedCallExecutor{executions: map[string]ToolOutcome{}, errors: map[string]error{}}, &scriptedProgress{})
	request := validRequest()
	request.Context.Replace(llm.UserMessage(strings.Repeat("x", 3000)))
	request.ModelInfo = llm.ModelInfo{ContextWindow: 1000, AutoCompactTokenLimit: 900, MaxOutputTokens: 400}
	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != StopFailed || !strings.Contains(result.Reason, "exceeds model context window") || len(iterator.inputs) != 0 {
		t.Fatalf("oversized Prompt was not rejected before sampling: result=%#v inputs=%#v", result, iterator.inputs)
	}
}

func TestRunnerPersistsToolCallsBeforeExecutionAndOutcomesInModelOrder(t *testing.T) {
	first := tool.NewCall("first", "read", []byte(`{"path":"first"}`))
	second := tool.NewCall("second", "read", []byte(`{"path":"second"}`))
	recorder := &orderingRolloutRecorder{}
	runner, err := NewRunner(
		&scriptedIterator{results: []IterationResult{toolIteration(first, second), candidateIteration("done")}},
		orderingExecutor{recorder: recorder}, &scriptedProgress{},
		RunnerOptions{Temperature: 0.2, MaxParallelTools: 2, Rollout: recorder},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), validRequest())
	if err != nil || result.StopReason != StopCompleted {
		t.Fatalf("run result=%#v err=%v", result, err)
	}
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	if !recorder.callsSaved || len(recorder.callMessage.ToolCalls) != 2 {
		t.Fatalf("Tool Calls were not persisted: %#v", recorder.callMessage)
	}
	if len(recorder.executed) != 2 {
		t.Fatalf("unexpected executions: %#v", recorder.executed)
	}
	if len(recorder.outcomeIDs) != 2 || recorder.outcomeIDs[0] != "first" || recorder.outcomeIDs[1] != "second" {
		t.Fatalf("outcome persistence order changed: %#v", recorder.outcomeIDs)
	}
}

func TestRunnerReplaysCanonicalToolArgumentsReturnedByExecutor(t *testing.T) {
	call := tool.NewCall("call-1", "read", []byte(`{"path":"README.md",`))
	canonical := tool.NewCall("call-1", "read", []byte(`{"path":"README.md"}`))
	iterator := &scriptedIterator{results: []IterationResult{toolIteration(call), candidateIteration("done")}}
	executor := &scriptedCallExecutor{executions: map[string]ToolOutcome{"call-1": successfulExecution(canonical, "contents")}, errors: map[string]error{}, canonical: map[string]tool.ToolCall{"call-1": canonical}}
	runner := newTestRunner(t, iterator, executor, &scriptedProgress{})
	result, err := runner.Run(context.Background(), validRequest())
	if err != nil || result.StopReason != StopCompleted {
		t.Fatalf("normalize call: result=%#v err=%v", result, err)
	}
	if len(executor.calls) != 1 || string(executor.calls[0].Payload) != `{"path":"README.md",` {
		t.Fatalf("Reactor changed arguments before the ToolExecutionService: %#v", executor.calls)
	}
	if got := string(iterator.inputs[1].Messages[1].ToolCalls[0].Arguments); got != `{"path":"README.md"}` {
		t.Fatalf("assistant replay did not use normalized arguments: %s", got)
	}
}

func TestRunnerReplaysArgumentFailureReturnedByExecutor(t *testing.T) {
	call := tool.NewCall("call-1", "read", []byte(`{"path":`))
	canonical := tool.NewCall("call-1", "read", []byte(`{}`))
	iterator := &scriptedIterator{results: []IterationResult{toolIteration(call), candidateIteration("recovered")}}
	failure := ToolOutcome{CallID: call.ID, ToolName: call.Name, Status: ToolOutcomeFailed, Error: &ToolError{Kind: "invalid_arguments", Message: "arguments are not valid JSON"}}
	executor := &scriptedCallExecutor{executions: map[string]ToolOutcome{call.ID: failure}, errors: map[string]error{}, canonical: map[string]tool.ToolCall{call.ID: canonical}}
	runner := newTestRunner(t, iterator, executor, &scriptedProgress{})
	result, err := runner.Run(context.Background(), validRequest())
	if err != nil || result.StopReason != StopCompleted {
		t.Fatalf("argument recovery: result=%#v err=%v", result, err)
	}
	if len(executor.calls) != 1 || len(result.Iterations) != 2 || result.Iterations[0].Intent != "tool_calls" || result.Iterations[0].Outcomes[0].Error.Kind != "invalid_arguments" {
		t.Fatalf("argument failure observation missing: calls=%#v result=%#v", executor.calls, result)
	}
	if len(iterator.inputs[1].Messages) != 3 || iterator.inputs[1].Messages[2].Role != llm.RoleTool {
		t.Fatalf("argument error was not replayed: %#v", iterator.inputs[1].Messages)
	}
}

func TestRunnerStopsBlockedOnPathBoundary(t *testing.T) {
	call := tool.NewCall("call-1", "read", []byte(`{"path":"../secret"}`))
	iterator := &scriptedIterator{results: []IterationResult{toolIteration(call)}}
	execution := successfulExecution(call, "")
	execution.Status = ToolOutcomeFailed
	execution.Error = &ToolError{Kind: "path_denied", Message: "path is outside project root"}
	execution.Blocking = true
	runner := newTestRunner(t, iterator, &scriptedCallExecutor{executions: map[string]ToolOutcome{call.ID: execution}, errors: map[string]error{}}, &scriptedProgress{})
	result, err := runner.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != StopBlocked || result.Reason != "path is outside project root" {
		t.Fatalf("unexpected blocked result: %#v", result)
	}
}

func TestRunnerReportsStalledWithoutRequestingPlan(t *testing.T) {
	call := tool.NewCall("call-1", "read", []byte(`{"path":"README.md"}`))
	iterator := &scriptedIterator{results: []IterationResult{toolIteration(call)}}
	progress := &scriptedProgress{signals: []ProgressSignal{{Kind: ProgressRepeatedAction, Reason: "same call repeated", RecommendPlan: true}}}
	runner := newTestRunner(t, iterator, &scriptedCallExecutor{executions: map[string]ToolOutcome{call.ID: successfulExecution(call, "contents")}, errors: map[string]error{}}, progress)
	result, err := runner.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != StopStalled || result.Reason != "same call repeated" {
		t.Fatalf("unexpected stalled result: %#v", result)
	}
}

func TestRunnerEnforcesIterationAndToolBudgets(t *testing.T) {
	request := validRequest()
	request.Budget.Budget.MaxIterations = 1
	request.Budget.IterationsUsed = 1
	runner := newTestRunner(t, &scriptedIterator{}, &scriptedCallExecutor{executions: map[string]ToolOutcome{}, errors: map[string]error{}}, &scriptedProgress{})
	result, err := runner.Run(context.Background(), request)
	if err != nil || result.StopReason != StopBudgetExhausted || result.Limit == nil || result.Limit.Limit != LimitIterations {
		t.Fatalf("unexpected iteration budget result: %#v err=%v", result, err)
	}

	call := tool.NewCall("call-1", "read", []byte(`{"path":"README.md"}`))
	request = validRequest()
	request.Budget.Budget.MaxToolCalls = 0
	request.Budget.ToolCallsUsed = 1
	request.Budget.Budget.MaxToolCalls = 1
	runner = newTestRunner(t, &scriptedIterator{results: []IterationResult{toolIteration(call)}}, &scriptedCallExecutor{executions: map[string]ToolOutcome{}, errors: map[string]error{}}, &scriptedProgress{})
	result, err = runner.Run(context.Background(), request)
	if err != nil || result.StopReason != StopBudgetExhausted || result.Limit == nil || result.Limit.Limit != LimitToolCalls {
		t.Fatalf("unexpected tool budget result: %#v err=%v", result, err)
	}
}

func TestRunnerReportsProviderFailureAndCancellation(t *testing.T) {
	runner := newTestRunner(t, &scriptedIterator{errors: []error{errors.New("provider unavailable")}}, &scriptedCallExecutor{executions: map[string]ToolOutcome{}, errors: map[string]error{}}, &scriptedProgress{})
	result, err := runner.Run(context.Background(), validRequest())
	if err != nil || result.StopReason != StopFailed || result.Reason != "provider unavailable" {
		t.Fatalf("unexpected provider failure: %#v err=%v", result, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = runner.Run(ctx, validRequest())
	if err != nil || result.StopReason != StopInterrupted {
		t.Fatalf("unexpected cancellation: %#v err=%v", result, err)
	}
}

func TestRunnerAccumulatesUsageWithoutReducingProviderRequestLimit(t *testing.T) {
	response := llm.Response{Message: llm.AssistantMessage("done"), FinishReason: llm.FinishReasonStop, Usage: llm.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}}
	candidate := response.Message
	iterator := &scriptedIterator{results: []IterationResult{{Kind: IterationCandidate, Response: response, Candidate: &candidate}}}
	request := validRequest()
	request.Budget.OutputTokensUsed = 4
	runner := newTestRunner(t, iterator, &scriptedCallExecutor{executions: map[string]ToolOutcome{}, errors: map[string]error{}}, &scriptedProgress{})
	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if iterator.inputs[0].MaxOutputTokens != 512 || result.Usage.TotalTokens != 5 || result.Budget.OutputTokensUsed != 6 {
		t.Fatalf("unexpected usage accounting: input=%#v result=%#v", iterator.inputs[0], result)
	}
}

func TestRunnerPublishesIterationLifecycleWithRunMetadata(t *testing.T) {
	response := llm.Response{Message: llm.AssistantMessage("done"), FinishReason: llm.FinishReasonStop}
	candidate := response.Message
	events := event.NewMemorySink()
	runner, err := NewRunner(
		&scriptedIterator{results: []IterationResult{{Kind: IterationCandidate, Response: response, Candidate: &candidate}}},
		&scriptedCallExecutor{executions: map[string]ToolOutcome{}, errors: map[string]error{}},
		&scriptedProgress{},
		RunnerOptions{
			Temperature: 0.2, MaxParallelTools: 2, Events: events,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := validRequest()
	request.ModelInfo.ContextWindow = 1000
	request.ModelInfo.AutoCompactTokenLimit = 850
	ctx := event.WithMetadata(context.Background(), event.Metadata{SessionID: "session-1"})
	if _, err := runner.Run(ctx, request); err != nil {
		t.Fatal(err)
	}
	snapshot := events.Snapshot()
	if len(snapshot) != 3 {
		t.Fatalf("unexpected lifecycle event count: %#v", snapshot)
	}
	started, ok := snapshot[0].(event.IterationStarted)
	if !ok || started.SessionID != "session-1" || started.TurnID != "run-1" || started.TaskID != "" || started.Iteration != 1 || started.LLMCallID != "run-1/llm-1" {
		t.Fatalf("unexpected iteration started event: %#v", snapshot[0])
	}
	contextUpdate, ok := snapshot[1].(event.ContextWindowUpdated)
	if !ok || contextUpdate.ContextWindow != 1000 || contextUpdate.EstimatedInputTokens <= 0 || contextUpdate.EffectiveInputLimit != 488 || contextUpdate.ProjectedToolResults != 0 || contextUpdate.DroppedMessagePairs != 0 || contextUpdate.TurnID != "run-1" || contextUpdate.Iteration != 1 || contextUpdate.LLMCallID != started.LLMCallID {
		t.Fatalf("unexpected context window event: %#v", snapshot[1])
	}
	completed, ok := snapshot[2].(event.IterationCompleted)
	if !ok || completed.Status != string(IterationCompleted) || completed.TurnID != "run-1" || completed.TaskID != "" || completed.LLMCallID != started.LLMCallID {
		t.Fatalf("unexpected iteration completed event: %#v", snapshot[2])
	}
}

func TestRunnerCompletesWithoutAvailableTools(t *testing.T) {
	response := llm.Response{Message: llm.AssistantMessage("hello"), FinishReason: llm.FinishReasonStop}
	candidate := response.Message
	runner := newTestRunner(t,
		&scriptedIterator{results: []IterationResult{{Kind: IterationCandidate, Response: response, Candidate: &candidate}}},
		&scriptedCallExecutor{executions: map[string]ToolOutcome{}, errors: map[string]error{}},
		&scriptedProgress{},
	)
	request := validRequest()
	request.AvailableTools = nil
	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != StopCompleted || result.FinalMessage == nil || result.FinalMessage.Content != "hello" || len(result.Iterations) != 1 {
		t.Fatalf("unexpected no-tools result: %#v", result)
	}
}

func newTestRunner(t *testing.T, iterator ModelIterator, executor CallExecutor, progress ProgressObserver) *Runner {
	t.Helper()
	runner, err := NewRunner(iterator, executor, progress, RunnerOptions{Temperature: 0.2, MaxParallelTools: 2})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func validRequest() Request {
	contextManager := agentcontext.NewManager(nil)
	contextManager.Record(llm.UserMessage("inspect repository"))
	return Request{
		TurnID: "run-1", Goal: "inspect repository", Context: contextManager,
		BaseInstructions: llm.BaseInstructions{Text: "You are Amadeus."},
		ModelInfo:        llm.ModelInfo{ContextWindow: 128_000, AutoCompactTokenLimit: 115_200, MaxOutputTokens: 512},
		AvailableTools:   []tool.ToolSpec{{Name: "read", Description: "read", InputSchema: []byte(`{"type":"object"}`), SideEffect: tool.SideEffectRead, Idempotent: true}},
		Budget:           BudgetState{Budget: Budget{MaxIterations: 8, MaxToolCalls: 8, MaxDuration: time.Minute}},
	}
}

func candidateIteration(content string) IterationResult {
	response := llm.Response{Message: llm.AssistantMessage(content), FinishReason: llm.FinishReasonStop}
	candidate := response.Message
	return IterationResult{Kind: IterationCandidate, Response: response, Candidate: &candidate}
}

func toolIteration(calls ...tool.ToolCall) IterationResult {
	message := llm.AssistantMessage("")
	for _, call := range calls {
		message.ToolCalls = append(message.ToolCalls, llm.ToolCall{ID: call.ID, Name: call.Name, Arguments: append([]byte(nil), call.Payload...)})
	}
	return IterationResult{Kind: IterationToolCalls, Response: llm.Response{Message: message, FinishReason: llm.FinishReasonToolCalls}, ToolCalls: calls}
}

func successfulExecution(call tool.ToolCall, text string) ToolOutcome {
	result := tool.Output{CallID: call.ID, ToolName: call.Name, Text: text}
	return ToolOutcome{CallID: call.ID, ToolName: call.Name, Status: ToolOutcomeSucceeded,
		Result: result,
	}
}

func executionFromOutcome(call tool.ToolCall, outcome ToolOutcome) tool.ToolExecution {
	status := tool.ToolCallFailed
	switch outcome.Status {
	case ToolOutcomeSucceeded:
		status = tool.ToolCallCompleted
	case ToolOutcomeDenied:
		status = tool.ToolCallDenied
	case ToolOutcomeInterrupted:
		status = tool.ToolCallInterrupted
	}
	var executionError *tool.ToolError
	if outcome.Error != nil {
		executionError = &tool.ToolError{Kind: outcome.Error.Kind, Message: outcome.Error.Message}
	}
	return tool.ToolExecution{
		Call:   call,
		Output: outcome.Result,
		Outcome: tool.ToolCallOutcome{
			Status: status, Error: executionError, Blocking: outcome.Blocking,
			Duration: outcome.Duration, Metadata: outcome.Metadata,
		},
	}
}
