package session

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestCoordinatorDelaysPersistenceUntilFirstTaskAndResumes(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	sequence := 0
	coordinator, err := NewCoordinator(store, t.TempDir(), "demo", CoordinatorOptions{
		Clock:     func() time.Time { now = now.Add(time.Second); return now },
		IDFactory: func(kind string) string { sequence++; return fmt.Sprintf("%s-%d", kind, sequence) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if sessions, err := coordinator.ListSessions(context.Background()); err != nil || len(sessions) != 0 {
		t.Fatalf("draft should not persist: sessions=%v err=%v", sessions, err)
	}
	started, err := coordinator.BeginTask(context.Background(), "implement feature", RunMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	interrupted, _ := EncodeInterruptedContext(InterruptedContextV1{Objective: "cancelled", Status: string(RunInterrupted), StopReason: "cancelled"})
	if _, err := coordinator.FinishTask(context.Background(), started, RunInterrupted, "cancelled", "", nil, interrupted); err != nil {
		t.Fatal(err)
	}
	second, err := coordinator.BeginTask(context.Background(), "please continue", RunMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Interrupted == nil || second.Records.Run.ContextFromRunID != started.Records.Run.ID || len(second.PriorMessages) != 1 {
		t.Fatalf("unexpected interrupted continuation: %#v", second)
	}
	if _, err := coordinator.FinishTask(context.Background(), second, RunCompleted, "", "done", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PendingInterruptedRun(context.Background(), second.Records.Session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("completed run should clear pending interruption: %v", err)
	}
}

func TestCoordinatorRecoversAbandonedRunningRunBeforeContinuation(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	sequence := 0
	projectPath := t.TempDir()
	newCoordinator := func() *Coordinator {
		coordinator, err := NewCoordinator(store, projectPath, "demo", CoordinatorOptions{
			Clock:     func() time.Time { now = now.Add(time.Second); return now },
			IDFactory: func(kind string) string { sequence++; return fmt.Sprintf("%s-%d", kind, sequence) },
		})
		if err != nil {
			t.Fatal(err)
		}
		return coordinator
	}
	firstCoordinator := newCoordinator()
	first, err := firstCoordinator.BeginTask(context.Background(), "unfinished task", RunMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	secondCoordinator := newCoordinator()
	if _, err := secondCoordinator.Continue(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := secondCoordinator.BeginTask(context.Background(), "continue task", RunMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Interrupted == nil || second.Interrupted.ID != first.Records.Run.ID || second.Records.Run.ContextFromRunID != first.Records.Run.ID {
		t.Fatalf("abandoned Run was not recovered into continuation: %#v", second)
	}
	recovered, err := store.GetRun(context.Background(), first.Records.Run.ID)
	if err != nil || recovered.Status != RunInterrupted || len(recovered.InterruptedContextJSON) == 0 {
		t.Fatalf("abandoned Run was not persisted as interrupted: %#v err=%v", recovered, err)
	}
}
