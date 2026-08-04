package plan

import (
	"context"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/agent/react"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type recordingReactor struct {
	metadata event.Metadata
	request  react.Request
	result   react.Result
}

func (reactor *recordingReactor) Run(ctx context.Context, request react.Request) (react.Result, error) {
	reactor.metadata = event.MetadataFromContext(ctx)
	reactor.request = request
	return reactor.result, nil
}

func TestReActTaskExecutorMapsPlanTaskWithoutLeakingPlanDomain(t *testing.T) {
	message := llm.AssistantMessage("task complete")
	reactor := &recordingReactor{result: react.Result{
		FinalMessage: &message,
		Evidence:     []react.Evidence{{ID: "evidence-1", Kind: react.EvidenceTool, Source: "read_file", Summary: "read", Verified: true}},
		Budget:       react.BudgetState{Budget: react.Budget{MaxIterations: 4}, IterationsUsed: 1},
		StopReason:   react.StopCompleted,
	}}
	executor, err := NewReActTaskExecutor(reactor)
	if err != nil {
		t.Fatal(err)
	}
	ctx := event.WithMetadata(context.Background(), event.Metadata{SessionID: "session-1", RunID: "run-1"})
	outcome, err := executor.Run(ctx, TaskRunInput{
		RunID: "run-1", Task: Task{ID: "task-1", Objective: "inspect", Status: TaskStatusRunning},
		Messages: []llm.Message{llm.UserMessage("inspect")}, Budget: BudgetState{Budget: Budget{MaxIterations: 4}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reactor.metadata.SessionID != "session-1" || reactor.metadata.RunID != "run-1" || reactor.metadata.TaskID != "task-1" {
		t.Fatalf("unexpected Reactor execution metadata: %#v", reactor.metadata)
	}
	if reactor.request.Metadata.TaskID != "task-1" || reactor.request.Goal != "inspect" || reactor.request.RunID != "run-1" {
		t.Fatalf("unexpected Reactor request: %#v", reactor.request)
	}
	if outcome.Kind != TaskOutcomeCandidateComplete || outcome.Candidate == nil || outcome.Candidate.Result.Summary != "task complete" || len(outcome.Evidence) != 1 {
		t.Fatalf("unexpected Task outcome: %#v", outcome)
	}
}
