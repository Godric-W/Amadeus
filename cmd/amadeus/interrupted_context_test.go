package main

import (
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/plan"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
)

func TestInterruptedContextFromResultCapturesBoundedPlanEvidence(t *testing.T) {
	goal := plan.Goal{Objective: "repair project"}
	state := plan.NewRun("run-1", goal, plan.ExecutionGraph{
		Kind: plan.ExecutionPlanned,
		Tasks: []plan.Task{
			{ID: "task-1", Objective: "inspect source", Status: plan.TaskStatusCompleted},
			{ID: "task-2", Objective: "run tests", Status: plan.TaskStatusBlocked},
		},
	}, plan.Budget{})
	state.Evidence = []plan.Evidence{{
		ID: "evidence-1", Summary: "read source", Artifact: &plan.ArtifactRef{Path: "main.go"},
	}}
	result := plan.PlanRunResult{State: state, Reason: "test command was cancelled"}
	encoded, err := interruptedContextFromResult(sessiondomain.RunInterrupted, "user cancelled", "repair project", result)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := sessiondomain.DecodePreviousWork(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Objective != "repair project" || decoded.LastError != "test command was cancelled" || len(decoded.CompletedWork) == 0 || len(decoded.PendingWork) == 0 || len(decoded.Evidence) != 1 || len(decoded.RelevantPaths) != 1 || decoded.RelevantPaths[0] != "main.go" || len(decoded.Usage) == 0 {
		t.Fatalf("unexpected interrupted context: %#v", decoded)
	}
}
