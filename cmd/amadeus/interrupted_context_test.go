package main

import (
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
)

func TestInterruptedContextFromResultCapturesBoundedPlanEvidence(t *testing.T) {
	goal := engine.Goal{Objective: "repair project"}
	state := engine.NewRun("run-1", goal, engine.ExecutionGraph{
		Kind: engine.ExecutionPlanned,
		Tasks: []engine.Task{
			{ID: "task-1", Objective: "inspect source", Status: engine.TaskStatusCompleted},
			{ID: "task-2", Objective: "run tests", Status: engine.TaskStatusBlocked},
		},
	}, engine.Budget{})
	state.Evidence = []engine.Evidence{{
		ID: "evidence-1", Summary: "read source", Artifact: &engine.ArtifactRef{Path: "main.go"},
	}}
	result := engine.DirectRunResult{
		State: state, Reason: "test command was cancelled",
		Steps: []engine.Step{{Index: 1, Status: engine.StepStatusCompleted, Decision: engine.DecisionSummary{NextAction: "inspect source"}}},
	}
	encoded, err := interruptedContextFromResult(sessiondomain.RunInterrupted, "user cancelled", "repair project", result)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := sessiondomain.DecodeInterruptedContext(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Objective != "repair project" || decoded.LastError != "test command was cancelled" || len(decoded.CompletedSteps) == 0 || len(decoded.PendingWork) == 0 || len(decoded.Evidence) != 1 || len(decoded.RelevantPaths) != 1 || decoded.RelevantPaths[0] != "main.go" || len(decoded.Usage) == 0 {
		t.Fatalf("unexpected interrupted context: %#v", decoded)
	}
}
