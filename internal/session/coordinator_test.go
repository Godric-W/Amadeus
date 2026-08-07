package session

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestCoordinatorUsesCanonicalHistoryAcrossInterruptedRun(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	counter := 0
	coordinator, err := NewCoordinator(store, t.TempDir(), "project", CoordinatorOptions{
		IDFactory: func(kind string) string { counter++; return fmt.Sprintf("%s-%d", kind, counter) },
		Clock:     func() time.Time { value := now; now = now.Add(time.Second); return value },
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	first, err := coordinator.BeginRun(context.Background(), "implement feature", RunMetadata{Mode: RunModeExecute})
	if err != nil {
		t.Fatalf("begin first run: %v", err)
	}
	if _, err := coordinator.FinishRun(context.Background(), first, RunInterrupted, "cancelled", "", nil); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	second, err := coordinator.BeginRun(context.Background(), "please continue", RunMetadata{Mode: RunModeExecute})
	if err != nil {
		t.Fatalf("begin second run: %v", err)
	}
	if len(second.PriorItems) != 2 || second.PriorItems[0].Kind != RolloutUserMessage || second.PriorItems[1].Kind != RolloutRunInterrupted {
		t.Fatalf("unexpected prior rollout: %#v", second.PriorItems)
	}
	if second.Records.Item.Sequence != 3 || second.Records.Run.Sequence != 2 {
		t.Fatalf("unexpected second sequences: %#v", second.Records)
	}
	if _, err := coordinator.FinishRun(context.Background(), second, RunCompleted, "", "done", nil); err != nil {
		t.Fatalf("complete run: %v", err)
	}
}
