package snapshot

import (
	"context"
	"testing"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type recordingService struct {
	begins    int
	completes int
}

func (service *recordingService) Begin(context.Context, string) (Snapshot, error) {
	service.begins++
	return Snapshot{}, nil
}

func (service *recordingService) Complete(context.Context, string) (Snapshot, error) {
	service.completes++
	return Snapshot{}, nil
}

func (*recordingService) Revert(context.Context, string) (RevertResult, error) {
	return RevertResult{}, nil
}

func TestRunTrackerStartsOnlyBeforeFirstWrite(t *testing.T) {
	service := &recordingService{}
	tracker, err := NewRunTracker(service, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := tracker.Before(ctx, tool.Spec{SideEffect: tool.SideEffectRead}, tool.Call{}); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	if service.begins != 0 || service.completes != 0 || tracker.Started() {
		t.Fatalf("read-only run captured snapshots: %#v", service)
	}
	for range 2 {
		if err := tracker.Before(ctx, tool.Spec{SideEffect: tool.SideEffectWrite}, tool.Call{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tracker.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	if service.begins != 1 || service.completes != 1 || !tracker.Started() {
		t.Fatalf("write run snapshot lifecycle mismatch: %#v", service)
	}
}
