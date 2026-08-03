package snapshot

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type RunTracker struct {
	service Service
	runID   string

	mutex   sync.Mutex
	started bool
}

func NewRunTracker(service Service, runID string) (*RunTracker, error) {
	if service == nil {
		return nil, errors.New("snapshot run tracker service is nil")
	}
	runID = strings.TrimSpace(runID)
	if !validRunID(runID) {
		return nil, errors.New("snapshot run tracker run ID is invalid")
	}
	return &RunTracker{service: service, runID: runID}, nil
}

func (tracker *RunTracker) Before(ctx context.Context, spec tool.Spec, _ tool.Call) error {
	if tracker == nil {
		return errors.New("snapshot run tracker is nil")
	}
	if spec.SideEffect != tool.SideEffectWrite {
		return nil
	}
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	if tracker.started {
		return nil
	}
	if _, err := tracker.service.Begin(ctx, tracker.runID); err != nil {
		return err
	}
	tracker.started = true
	return nil
}

func (tracker *RunTracker) Complete(ctx context.Context) (Snapshot, error) {
	if tracker == nil {
		return Snapshot{}, errors.New("snapshot run tracker is nil")
	}
	tracker.mutex.Lock()
	started := tracker.started
	tracker.mutex.Unlock()
	if !started {
		return Snapshot{RunID: tracker.runID}, nil
	}
	return tracker.service.Complete(ctx, tracker.runID)
}

func (tracker *RunTracker) Started() bool {
	if tracker == nil {
		return false
	}
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	return tracker.started
}
