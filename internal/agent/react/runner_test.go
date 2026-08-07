package react

import (
	"context"
	"errors"
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
	calls      []tool.Call
}

func (executor *scriptedCallExecutor) Execute(_ context.Context, call tool.Call) (ToolOutcome, error) {
	executor.calls = append(executor.calls, call)
	return executor.executions[call.ID], executor.errors[call.ID]
}

type scriptedProgress struct {
	signals []ProgressSignal
	samples []ProgressSample
}

type fixedContextWindowManager struct {
	view agentcontext.RequestView
	err  error
}

type recordingRequestViewProvider struct {
	inputs []RequestViewInput
	tools  []tool.Spec
}

func (provider *recordingRequestViewProvider) PrepareRequestView(_ context.Context, input RequestViewInput) (agentcontext.RequestView, error) {
	provider.inputs = append(provider.inputs, input)
	messages := []llm.Message{llm.UserMessage("inspect repository")}
	messages = append(messages, input.RuntimeMessages...)
	return agentcontext.RequestView{Messages: messages, Tools: append([]tool.Spec(nil), provider.tools...)}, nil
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

func (executor orderingExecutor) Execute(_ context.Context, call tool.Call) (ToolOutcome, error) {
	executor.recorder.mutex.Lock()
	if !executor.recorder.callsSaved {
		executor.recorder.mutex.Unlock()
		return ToolOutcome{}, errors.New("Tool Call executed before rollout persistence")
	}
	executor.recorder.executed = append(executor.recorder.executed, call.ID)
	executor.recorder.mutex.Unlock()
	if call.ID == "first" {
		time.Sleep(20 * time.Millisecond)
	}
	return successfulExecution(call, call.ID), nil
}

func (manager fixedContextWindowManager) Prepare(context.Context, agentcontext.WindowRequest) (agentcontext.RequestView, error) {
	return manager.view, manager.err
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
	call := tool.NewCall("call-1", "read_file", []byte(`{"path":"README.md"}`))
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

func TestRunnerBuildsFreshRequestViewBeforeEveryThink(t *testing.T) {
	call := tool.NewCall("call-1", "read_file", []byte(`{"path":"README.md"}`))
	iterator := &scriptedIterator{results: []IterationResult{toolIteration(call), candidateIteration("done")}}
	executor := &scriptedCallExecutor{executions: map[string]ToolOutcome{call.ID: successfulExecution(call, "contents")}, errors: map[string]error{}}
	provider := &recordingRequestViewProvider{tools: validRequest().AvailableTools}
	runner := newTestRunner(t, iterator, executor, &scriptedProgress{})
	request := validRequest()
	request.Messages = nil
	request.AvailableTools = nil
	request.RequestViewProvider = provider
	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != StopCompleted || len(provider.inputs) != 2 {
		t.Fatalf("unexpected dynamic request result: result=%#v samples=%d", result, len(provider.inputs))
	}
	if len(provider.inputs[0].RuntimeMessages) != 0 || len(provider.inputs[1].RuntimeMessages) != 2 {
		t.Fatalf("request views did not receive per-iteration runtime state: %#v", provider.inputs)
	}
	if len(iterator.inputs) != 2 || len(iterator.inputs[1].Messages) != 3 || iterator.inputs[1].Messages[2].Role != llm.RoleTool {
		t.Fatalf("fresh request view did not drive the next Think: %#v", iterator.inputs)
	}
	if len(executor.calls) != 1 || executor.calls[0].Name != "read_file" {
		t.Fatalf("Act did not use the Tool snapshot from the sampled request view: %#v", executor.calls)
	}
}

func TestRunnerPersistsToolCallsBeforeExecutionAndOutcomesInModelOrder(t *testing.T) {
	first := tool.NewCall("first", "read_file", []byte(`{"path":"first"}`))
	second := tool.NewCall("second", "read_file", []byte(`{"path":"second"}`))
	recorder := &orderingRolloutRecorder{}
	runner, err := NewRunner(
		&scriptedIterator{results: []IterationResult{toolIteration(first, second), candidateIteration("done")}},
		orderingExecutor{recorder: recorder}, &scriptedProgress{},
		RunnerOptions{Temperature: 0.2, MaxOutputTokens: 512, MaxParallelTools: 2, Rollout: recorder},
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

func TestRunnerNormalizesToolArgumentsBeforeExecutionAndReplay(t *testing.T) {
	call := tool.NewCall("call-1", "read_file", []byte(`{"path":"README.md",`))
	iterator := &scriptedIterator{results: []IterationResult{toolIteration(call), candidateIteration("done")}}
	executor := &scriptedCallExecutor{executions: map[string]ToolOutcome{"call-1": successfulExecution(call, "contents")}, errors: map[string]error{}}
	runner := newTestRunner(t, iterator, executor, &scriptedProgress{})
	result, err := runner.Run(context.Background(), validRequest())
	if err != nil || result.StopReason != StopCompleted {
		t.Fatalf("normalize call: result=%#v err=%v", result, err)
	}
	if len(executor.calls) != 1 || string(executor.calls[0].Arguments) != `{"path":"README.md"}` {
		t.Fatalf("executor did not receive normalized arguments: %#v", executor.calls)
	}
	if got := string(iterator.inputs[1].Messages[1].ToolCalls[0].Arguments); got != `{"path":"README.md"}` {
		t.Fatalf("assistant replay did not use normalized arguments: %s", got)
	}
}

func TestRunnerReturnsArgumentErrorObservationWithoutExecutingTool(t *testing.T) {
	call := tool.NewCall("call-1", "read_file", []byte(`{"path":`))
	iterator := &scriptedIterator{results: []IterationResult{toolIteration(call), candidateIteration("recovered")}}
	executor := &scriptedCallExecutor{executions: map[string]ToolOutcome{}, errors: map[string]error{}}
	runner := newTestRunner(t, iterator, executor, &scriptedProgress{})
	result, err := runner.Run(context.Background(), validRequest())
	if err != nil || result.StopReason != StopCompleted {
		t.Fatalf("argument recovery: result=%#v err=%v", result, err)
	}
	if len(executor.calls) != 0 || len(result.Iterations) != 2 || result.Iterations[0].Intent != "tool_argument_error" {
		t.Fatalf("invalid call reached executor or observation missing: calls=%#v result=%#v", executor.calls, result)
	}
	if len(iterator.inputs[1].Messages) != 3 || iterator.inputs[1].Messages[2].Role != llm.RoleTool {
		t.Fatalf("argument error was not replayed: %#v", iterator.inputs[1].Messages)
	}
}

func TestRunnerStopsBlockedOnPathBoundary(t *testing.T) {
	call := tool.NewCall("call-1", "read_file", []byte(`{"path":"../secret"}`))
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
	call := tool.NewCall("call-1", "read_file", []byte(`{"path":"README.md"}`))
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

	call := tool.NewCall("call-1", "read_file", []byte(`{"path":"README.md"}`))
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

func TestRunnerAccumulatesUsageAndRespectsOutputRemainder(t *testing.T) {
	response := llm.Response{Message: llm.AssistantMessage("done"), FinishReason: llm.FinishReasonStop, Usage: llm.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}}
	candidate := response.Message
	iterator := &scriptedIterator{results: []IterationResult{{Kind: IterationCandidate, Response: response, Candidate: &candidate}}}
	request := validRequest()
	request.Budget.OutputTokensUsed = 4
	request.Budget.Budget.MaxOutputTokens = 6
	runner := newTestRunner(t, iterator, &scriptedCallExecutor{executions: map[string]ToolOutcome{}, errors: map[string]error{}}, &scriptedProgress{})
	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if iterator.inputs[0].MaxOutputTokens != 2 || result.Usage.TotalTokens != 5 || result.Budget.OutputTokensUsed != 6 {
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
			Temperature: 0.2, MaxOutputTokens: 512, MaxParallelTools: 2, Events: events,
			ContextProfile: agentcontext.ContextProfile{ContextWindow: 1000, OutputReserve: 100, SafetyMargin: 50, CompressAt: 0.8},
			ContextWindow: fixedContextWindowManager{view: agentcontext.RequestView{
				Messages:   []llm.Message{llm.UserMessage("goal")},
				Usage:      agentcontext.ContextUsage{EstimatedInputTokens: 400, EffectiveInputLimit: 850},
				Compaction: &agentcontext.CompactionReport{ProjectedToolResults: 2, DroppedMessagePairs: 3},
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := validRequest()
	ctx := event.WithMetadata(context.Background(), event.Metadata{SessionID: "session-1"})
	if _, err := runner.Run(ctx, request); err != nil {
		t.Fatal(err)
	}
	snapshot := events.Snapshot()
	if len(snapshot) != 3 {
		t.Fatalf("unexpected lifecycle event count: %#v", snapshot)
	}
	started, ok := snapshot[0].(event.IterationStarted)
	if !ok || started.SessionID != "session-1" || started.RunID != "run-1" || started.TaskID != "" || started.Iteration != 1 || started.LLMCallID != "run-1/llm-1" {
		t.Fatalf("unexpected iteration started event: %#v", snapshot[0])
	}
	contextUpdate, ok := snapshot[1].(event.ContextWindowUpdated)
	if !ok || contextUpdate.ContextWindow != 1000 || contextUpdate.EstimatedInputTokens != 400 || contextUpdate.EffectiveInputLimit != 850 || contextUpdate.ProjectedToolResults != 2 || contextUpdate.DroppedMessagePairs != 3 || contextUpdate.RunID != "run-1" || contextUpdate.Iteration != 1 || contextUpdate.LLMCallID != started.LLMCallID {
		t.Fatalf("unexpected context window event: %#v", snapshot[1])
	}
	completed, ok := snapshot[2].(event.IterationCompleted)
	if !ok || completed.Status != string(IterationCompleted) || completed.RunID != "run-1" || completed.TaskID != "" || completed.LLMCallID != started.LLMCallID {
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
	runner, err := NewRunner(iterator, executor, progress, RunnerOptions{Temperature: 0.2, MaxOutputTokens: 512, MaxParallelTools: 2})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func validRequest() Request {
	return Request{
		RunID: "run-1", Goal: "inspect repository", Messages: []llm.Message{llm.UserMessage("inspect repository")},
		AvailableTools: []tool.Spec{{Name: "read_file", Description: "read", InputSchema: []byte(`{"type":"object"}`), SideEffect: tool.SideEffectRead, Concurrency: tool.ToolConcurrencyShared, Idempotent: true}},
		Budget:         BudgetState{Budget: Budget{MaxIterations: 8, MaxToolCalls: 8, MaxInputTokens: 1000, MaxOutputTokens: 1000, MaxDuration: time.Minute}},
	}
}

func candidateIteration(content string) IterationResult {
	response := llm.Response{Message: llm.AssistantMessage(content), FinishReason: llm.FinishReasonStop}
	candidate := response.Message
	return IterationResult{Kind: IterationCandidate, Response: response, Candidate: &candidate}
}

func toolIteration(calls ...tool.Call) IterationResult {
	message := llm.AssistantMessage("")
	for _, call := range calls {
		message.ToolCalls = append(message.ToolCalls, llm.ToolCall{ID: call.ID, Name: call.Name, Arguments: append([]byte(nil), call.Arguments...)})
	}
	return IterationResult{Kind: IterationToolCalls, Response: llm.Response{Message: message, FinishReason: llm.FinishReasonToolCalls}, ToolCalls: calls}
}

func successfulExecution(call tool.Call, text string) ToolOutcome {
	result := tool.Result{CallID: call.ID, ToolName: call.Name, Text: text}
	return ToolOutcome{CallID: call.ID, ToolName: call.Name, Status: ToolOutcomeSucceeded,
		Result: result,
	}
}
