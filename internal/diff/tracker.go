package diff

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	patchtool "github.com/Godric-W/Amadeus/internal/tool/patch"
)

type ChangeKind string

const (
	ChangeAdded   ChangeKind = "added"
	ChangeUpdated ChangeKind = "updated"
	ChangeDeleted ChangeKind = "deleted"
	ChangeMoved   ChangeKind = "moved"
)

type Change struct {
	Path         string     `json:"path"`
	PreviousPath string     `json:"previous_path,omitempty"`
	Kind         ChangeKind `json:"kind"`
	Bytes        int        `json:"bytes,omitempty"`
}

type Snapshot struct {
	Changes     []Change `json:"changes,omitempty"`
	Invalidated bool     `json:"invalidated"`
	Reason      string   `json:"reason,omitempty"`
	Revision    int64    `json:"revision"`
}

type Projector struct {
	mutex       sync.RWMutex
	cwd         string
	changes     map[string]Change
	invalidated bool
	reason      string
	revision    int64
}

func NewProjector(cwd string) (*Projector, error) {
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	if cwd == "" || !filepath.IsAbs(cwd) {
		return nil, errors.New("Run Diff Tracker cwd must be absolute")
	}
	return &Projector{cwd: cwd, changes: make(map[string]Change)}, nil
}

func (tracker *Projector) ProjectPatch(ctx context.Context, deltas []patchtool.AppliedPatchDelta) error {
	if tracker == nil {
		return errors.New("Run Diff Projector is nil")
	}
	if len(deltas) == 0 {
		return nil
	}
	for _, delta := range deltas {
		if err := tracker.validateDelta(delta); err != nil {
			return tracker.invalidate(ctx, err.Error())
		}
	}
	tracker.mutex.Lock()
	for _, delta := range deltas {
		if err := tracker.applyDelta(delta); err != nil {
			tracker.mutex.Unlock()
			return tracker.invalidate(ctx, err.Error())
		}
	}
	tracker.revision++
	tracker.mutex.Unlock()
	return nil
}

func (tracker *Projector) validateDelta(delta patchtool.AppliedPatchDelta) error {
	if !delta.Exact {
		return fmt.Errorf("apply_patch delta for %q is not exact", delta.Path)
	}
	if strings.TrimSpace(delta.UnifiedDiff) == "" {
		return fmt.Errorf("apply_patch delta for %q has no unified diff", delta.Path)
	}
	if _, err := tracker.canonical(delta.Path); err != nil {
		return err
	}
	switch delta.Operation {
	case patchtool.OperationAdd, patchtool.OperationUpdate, patchtool.OperationDelete:
		if strings.TrimSpace(delta.Destination) != "" {
			return fmt.Errorf("apply_patch %s delta for %q has unexpected destination %q", delta.Operation, delta.Path, delta.Destination)
		}
	case patchtool.OperationMove:
		if _, err := tracker.canonical(delta.Destination); err != nil {
			return fmt.Errorf("apply_patch move destination: %w", err)
		}
	default:
		return fmt.Errorf("apply_patch delta for %q has unsupported operation %q", delta.Path, delta.Operation)
	}
	return nil
}

func (tracker *Projector) Snapshot() Snapshot {
	if tracker == nil {
		return Snapshot{}
	}
	tracker.mutex.RLock()
	defer tracker.mutex.RUnlock()
	return tracker.snapshotLocked()
}

func (tracker *Projector) applyDelta(delta patchtool.AppliedPatchDelta) error {
	path, err := tracker.canonical(delta.Path)
	if err != nil {
		return err
	}
	bytes := len(delta.NewContent)
	if delta.Operation == patchtool.OperationMove {
		destination, err := tracker.canonical(delta.Destination)
		if err != nil {
			return err
		}
		current, exists := tracker.changes[path]
		delete(tracker.changes, path)
		switch {
		case exists && current.Kind == ChangeAdded:
			current.Path = destination
			current.Bytes = bytes
			tracker.changes[destination] = current
		case exists && current.Kind == ChangeMoved:
			current.Path = destination
			current.Bytes = bytes
			tracker.changes[destination] = current
		default:
			tracker.changes[destination] = Change{Path: destination, PreviousPath: path, Kind: ChangeMoved, Bytes: bytes}
		}
		return nil
	}
	current, exists := tracker.changes[path]
	switch delta.Operation {
	case patchtool.OperationAdd:
		tracker.changes[path] = Change{Path: path, Kind: ChangeAdded, Bytes: bytes}
	case patchtool.OperationDelete:
		if exists && current.Kind == ChangeAdded {
			delete(tracker.changes, path)
		} else if exists && current.Kind == ChangeMoved {
			delete(tracker.changes, path)
			tracker.changes[current.PreviousPath] = Change{Path: current.PreviousPath, Kind: ChangeDeleted}
		} else {
			tracker.changes[path] = Change{Path: path, Kind: ChangeDeleted}
		}
	case patchtool.OperationUpdate:
		if exists && (current.Kind == ChangeAdded || current.Kind == ChangeMoved) {
			current.Bytes = bytes
			tracker.changes[path] = current
		} else {
			tracker.changes[path] = Change{Path: path, Kind: ChangeUpdated, Bytes: bytes}
		}
	default:
		return fmt.Errorf("apply_patch delta for %q has unsupported operation %q", delta.Path, delta.Operation)
	}
	return nil
}

func (tracker *Projector) canonical(value string) (string, error) {
	value = filepath.Clean(strings.TrimSpace(value))
	if value == "" || value == "." {
		return "", fmt.Errorf("Run Diff path %q is invalid", value)
	}
	if filepath.IsAbs(value) {
		return value, nil
	}
	return filepath.Clean(filepath.Join(tracker.cwd, value)), nil
}

func (tracker *Projector) invalidate(ctx context.Context, reason string) error {
	reason = strings.TrimSpace(reason)
	tracker.mutex.Lock()
	if tracker.invalidated {
		tracker.mutex.Unlock()
		return nil
	}
	tracker.invalidated = true
	tracker.reason = reason
	tracker.revision++
	tracker.mutex.Unlock()
	return nil
}

func (tracker *Projector) snapshotLocked() Snapshot {
	changes := make([]Change, 0, len(tracker.changes))
	for _, change := range tracker.changes {
		changes = append(changes, change)
	}
	sort.Slice(changes, func(left, right int) bool { return changes[left].Path < changes[right].Path })
	return Snapshot{Changes: changes, Invalidated: tracker.invalidated, Reason: tracker.reason, Revision: tracker.revision}
}

var _ interface {
	ProjectPatch(context.Context, []patchtool.AppliedPatchDelta) error
} = (*Projector)(nil)
