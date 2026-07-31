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
	if _, err := coordinator.FinishTask(context.Background(), started, RunCancelled, "cancelled", "", nil); err != nil {
		t.Fatal(err)
	}
	second, err := coordinator.BeginTask(context.Background(), "please continue", RunMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Interrupted == nil || second.Records.Run.ContextFromRunID != started.Records.Run.ID || len(second.PriorMessages) != 1 {
		t.Fatalf("unexpected interrupted continuation: %#v", second)
	}
	if _, err := coordinator.FinishTask(context.Background(), second, RunCompleted, "", "done", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PendingInterruptedRun(context.Background(), second.Records.Session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("completed run should clear pending interruption: %v", err)
	}
}
