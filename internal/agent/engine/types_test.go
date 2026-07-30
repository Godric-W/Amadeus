package engine

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestDirectGraphCreatesSingleRootTask(t *testing.T) {
	goal := Goal{
		Objective:          "fix the failing test",
		AcceptanceCriteria: []Criterion{{ID: "tests", Description: "tests pass", Required: true}},
	}
	graph := NewDirectGraph(goal)
	if graph.Kind != ExecutionDirect || graph.Version != 1 || len(graph.Tasks) != 1 {
		t.Fatalf("unexpected direct graph: %#v", graph)
	}
	root := graph.Tasks[0]
	if root.ID != TaskID("root") || root.Objective != goal.Objective || root.Status != TaskStatusPending {
		t.Fatalf("unexpected root task: %#v", root)
	}
	graph.Tasks[0].AcceptanceCriteria[0].Description = "changed"
	if goal.AcceptanceCriteria[0].Description != "tests pass" {
		t.Fatal("direct graph shares goal acceptance criteria")
	}
}

func TestRunStateJSONRoundTripPreservesDomainData(t *testing.T) {
	completedAt := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	state := RunState{
		ID:   RunID("run_1"),
		Goal: Goal{Objective: "update code"},
		Graph: ExecutionGraph{Kind: ExecutionPlanned, Version: 2, Tasks: []Task{
			{ID: TaskID("inspect"), Objective: "inspect", Status: TaskStatusCompleted},
			{ID: TaskID("change"), Objective: "change", Dependencies: []TaskID{"inspect"}, Status: TaskStatusRunning},
		}},
		ActiveTaskID: TaskID("change"),
		Status:       RunStatusTaskRunning,
		Budget:       BudgetState{Budget: Budget{MaxSteps: 10}, StepsUsed: 2},
		Evidence: []Evidence{{
			ID: EvidenceID("evidence_1"), Kind: EvidenceTest, Source: "go test", Summary: "passed", Verified: true,
		}},
		Reflections: []Reflection{{Scope: ReflectionScopeTask, Verdict: ReflectionRetry, Issues: []Issue{{Code: "missing_fixture", Summary: "missing fixture", Severity: IssueSeverityWarning}}}},
	}
	state.Graph.Tasks[1].Result = &TaskResult{Summary: "partial", Partial: true}
	state.Graph.Tasks[1].Budget = Budget{MaxDuration: time.Minute}
	state.Graph.Tasks[1].Result.EvidenceIDs = []EvidenceID{"evidence_1"}
	_ = Step{CompletedAt: &completedAt, ToolCalls: []tool.Call{tool.NewCall("call_1", "read_file", json.RawMessage(`{"path":"a"}`))}}

	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal run state: %v", err)
	}
	var decoded RunState
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal run state: %v", err)
	}
	if !reflect.DeepEqual(decoded, state) {
		t.Fatalf("round trip changed state:\n got: %#v\nwant: %#v", decoded, state)
	}
}

func TestExecutionKindValidation(t *testing.T) {
	for _, kind := range []ExecutionKind{ExecutionDirect, ExecutionPlanned, ExecutionDelegated} {
		if !kind.Valid() {
			t.Fatalf("known execution kind is invalid: %q", kind)
		}
	}
	if ExecutionKind("manual").Valid() {
		t.Fatal("unknown execution kind is valid")
	}
}
