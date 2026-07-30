package engine_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/agent/react"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/tool/builtin"
)

type e2eIterator struct {
	results []react.IterationResult
	inputs  []react.IterationInput
}

func (iterator *e2eIterator) Run(_ context.Context, input react.IterationInput) (react.IterationResult, error) {
	iterator.inputs = append(iterator.inputs, input)
	result := iterator.results[0]
	iterator.results = iterator.results[1:]
	return result, nil
}

type criterionCallExecutor struct {
	delegate *react.ToolExecutor
}

func (executor *criterionCallExecutor) Execute(ctx context.Context, call tool.Call) (react.ToolExecution, error) {
	execution, err := executor.delegate.Execute(ctx, call)
	if call.Name == "execute_command" && err == nil {
		execution.Evidence.CriterionIDs = []string{"tests"}
		execution.Evidence.Kind = engine.EvidenceTest
	}
	return execution, err
}

type acceptingReflector struct{}

func (acceptingReflector) Reflect(_ context.Context, input engine.ReflectionInput) (engine.Reflection, error) {
	return engine.Reflection{Scope: engine.ReflectionScopeTask, Verdict: engine.ReflectionAccept, Lesson: "read, edit, and verify with deterministic evidence"}, nil
}

func TestDirectEngineReadsWritesTestsVerifiesAndReflects(t *testing.T) {
	projectPath := t.TempDir()
	writeFixture(t, filepath.Join(projectPath, "go.mod"), "module example.com/calc\n\ngo 1.26.0\n")
	writeFixture(t, filepath.Join(projectPath, "calc.go"), "package calc\n\nfunc Add(left, right int) int { return left - right }\n")
	writeFixture(t, filepath.Join(projectPath, "calc_test.go"), "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 { t.Fatal(\"unexpected sum\") }\n}\n")
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	options := builtin.DefaultMVPOptions()
	options.GrepCode.DisableRipgrep = true
	options.ExecuteCommand.DefaultTimeout = 30 * time.Second
	options.ExecuteCommand.MaxTimeout = 30 * time.Second
	registry, err := builtin.NewMVPRegistry(root, options)
	if err != nil {
		t.Fatalf("create MVP registry: %v", err)
	}
	toolExecutor, err := react.NewToolExecutor(registry, tool.NewArgumentValidator())
	if err != nil {
		t.Fatalf("create tool executor: %v", err)
	}

	readCall := tool.NewCall("read_calc", "read_file", json.RawMessage(`{"path":"calc.go"}`))
	writeCall := tool.NewCall("write_calc", "write_file", json.RawMessage(`{"path":"calc.go","content":"package calc\n\nfunc Add(left, right int) int { return left + right }\n"}`))
	testCall := tool.NewCall("run_tests", "execute_command", json.RawMessage(`{"command":"GOCACHE=\"$PWD/.gocache\" GOMODCACHE=\"$PWD/.gomodcache\" go test ./...","timeout_ms":30000}`))
	iterator := &e2eIterator{results: []react.IterationResult{
		toolIteration(readCall),
		toolIteration(writeCall),
		toolIteration(testCall),
		{
			Kind:      react.IterationCandidate,
			Response:  llm.Response{Message: llm.AssistantMessage("fixed Add and verified the project tests"), FinishReason: llm.FinishReasonStop},
			Candidate: &engine.TaskResult{Summary: "fixed Add and verified the project tests"},
		},
	}}
	runner, err := react.NewRunner(iterator, &criterionCallExecutor{delegate: toolExecutor}, react.DefaultProgressMonitor(), react.RunnerOptions{
		Temperature: 0.1, MaxOutputTokens: 512, MaxParallelTools: 2,
	})
	if err != nil {
		t.Fatalf("create ReAct runner: %v", err)
	}
	sink := event.NewMemorySink()
	direct, err := engine.NewDirectEngine(runner, engine.NewDeterministicVerifier(), acceptingReflector{}, sink, engine.DirectEngineOptions{MaxAttempts: 2})
	if err != nil {
		t.Fatalf("create direct engine: %v", err)
	}
	goal := engine.Goal{
		Objective:          "fix Add and ensure tests pass",
		AcceptanceCriteria: []engine.Criterion{{ID: "tests", Description: "project tests pass", Required: true}},
	}
	state := engine.NewRun("e2e_run", goal, engine.NewDirectGraph(goal), engine.Budget{
		MaxSteps: 8, MaxToolCalls: 6, MaxInputTokens: 10_000, MaxOutputTokens: 10_000, MaxDuration: time.Minute,
	})
	availableTools := make([]tool.Spec, 0, registry.Len())
	for _, entry := range registry.Snapshot() {
		availableTools = append(availableTools, entry.Spec)
	}

	result, err := direct.Run(context.Background(), engine.DirectRunInput{
		State: state, Messages: []llm.Message{llm.UserMessage(goal.Objective)}, AvailableTools: availableTools,
	})
	if err != nil {
		t.Fatalf("run direct engine end-to-end: %v", err)
	}
	rootTask := result.State.Graph.Tasks[0]
	if result.State.Status != engine.RunStatusCompleted || result.State.StopReason != engine.StopReasonCompleted || rootTask.Status != engine.TaskStatusCompleted {
		t.Fatalf("direct engine did not complete: %#v", result.State)
	}
	if result.Verification == nil || !result.Verification.Passed() || result.Reflection == nil || result.Reflection.Verdict != engine.ReflectionAccept {
		t.Fatalf("quality gates did not accept: verification=%#v reflection=%#v", result.Verification, result.Reflection)
	}
	if len(result.Steps) != 4 || len(result.State.Evidence) != 3 || len(iterator.inputs) != 4 {
		t.Fatalf("unexpected execution trace: steps=%d evidence=%d iterations=%d", len(result.Steps), len(result.State.Evidence), len(iterator.inputs))
	}
	updated, err := os.ReadFile(filepath.Join(projectPath, "calc.go"))
	if err != nil || !strings.Contains(string(updated), "left + right") {
		t.Fatalf("project file was not updated: %q, %v", updated, err)
	}
	if !containsTestEvidence(result.State.Evidence) {
		t.Fatalf("passing test evidence missing: %#v", result.State.Evidence)
	}
	events := sink.Snapshot()
	if len(events) == 0 || events[len(events)-1].Type() != event.TypeEngineRunCompleted {
		t.Fatalf("terminal engine event missing: %#v", events)
	}
}

func toolIteration(call tool.Call) react.IterationResult {
	return react.IterationResult{
		Kind: react.IterationToolCalls,
		Response: llm.Response{
			Message:      llm.AssistantToolCallMessage("", llm.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}),
			FinishReason: llm.FinishReasonToolCalls,
		},
		ToolCalls: []tool.Call{call},
	}
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func containsTestEvidence(evidence []engine.Evidence) bool {
	for _, item := range evidence {
		if item.Kind == engine.EvidenceTest && item.Verified && len(item.CriterionIDs) == 1 && item.CriterionIDs[0] == "tests" {
			return true
		}
	}
	return false
}
