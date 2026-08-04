package plan

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestPlanGraphStartsWithoutSyntheticRootTask(t *testing.T) {
	graph := NewPlanGraph()
	if graph.Kind != ExecutionPlanned || graph.Version != 1 || len(graph.Tasks) != 0 {
		t.Fatalf("unexpected initial plan graph: %#v", graph)
	}
}

func TestRunStateJSONRoundTripPreservesPlanData(t *testing.T) {
	state := RunState{
		ID:   RunID("run_1"),
		Goal: Goal{Objective: "update code"},
		Graph: ExecutionGraph{Kind: ExecutionPlanned, Version: 2, Tasks: []Task{
			{ID: TaskID("inspect"), Objective: "inspect", Status: TaskStatusCompleted},
			{ID: TaskID("change"), Objective: "change", Dependencies: []TaskID{"inspect"}, Status: TaskStatusRunning},
		}},
		ActiveTaskID: TaskID("change"),
		Status:       RunStatusTaskRunning,
		Budget:       BudgetState{Budget: Budget{MaxIterations: 10}, IterationsUsed: 2},
		Evidence: []Evidence{{
			ID: EvidenceID("evidence_1"), Kind: EvidenceTest, Source: "go test", Summary: "passed", Verified: true,
		}},
	}
	state.Graph.Tasks[1].Result = &TaskResult{Summary: "partial", Partial: true, EvidenceIDs: []EvidenceID{"evidence_1"}}
	state.Graph.Tasks[1].Budget = Budget{MaxDuration: time.Minute}

	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal run state: %v", err)
	}
	if string(encoded) == "" || !containsJSONKey(encoded, "iterations_used") || containsJSONKey(encoded, "steps_used") {
		t.Fatalf("run state uses stale iteration JSON field: %s", encoded)
	}
	var decoded RunState
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal run state: %v", err)
	}
	if !reflect.DeepEqual(decoded, state) {
		t.Fatalf("round trip changed state:\n got: %#v\nwant: %#v", decoded, state)
	}
}

func containsJSONKey(encoded []byte, key string) bool {
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return false
	}
	budget, _ := value["budget"].(map[string]any)
	_, ok := budget[key]
	return ok
}

func TestExecutionKindValidationOnlyAcceptsPlanned(t *testing.T) {
	if !ExecutionPlanned.Valid() {
		t.Fatal("planned execution kind is invalid")
	}
	for _, kind := range []ExecutionKind{"direct", "delegated", "manual"} {
		if kind.Valid() {
			t.Fatalf("legacy execution kind is valid: %q", kind)
		}
	}
}
