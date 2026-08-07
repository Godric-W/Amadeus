package diff

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/tool"
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

type Tracker struct {
	mutex       sync.RWMutex
	cwd         string
	events      event.Sink
	changes     map[string]Change
	invalidated bool
	reason      string
	revision    int64
}

func NewTracker(cwd string, events event.Sink) (*Tracker, error) {
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	if cwd == "" || !filepath.IsAbs(cwd) {
		return nil, errors.New("Run Diff Tracker cwd must be absolute")
	}
	if events == nil {
		return nil, errors.New("Run Diff Tracker event sink is nil")
	}
	return &Tracker{cwd: cwd, events: events, changes: make(map[string]Change)}, nil
}

func (tracker *Tracker) After(ctx context.Context, spec tool.Spec, _ tool.Call, result tool.Result) error {
	if tracker == nil {
		return errors.New("Run Diff Tracker is nil")
	}
	switch spec.Name {
	case "apply_patch":
		operations, err := decodeOperations(result.Metadata["operations"])
		if err != nil {
			return tracker.invalidate(ctx, "apply_patch returned unreadable change metadata: "+err.Error())
		}
		if len(operations) == 0 {
			return nil
		}
		tracker.mutex.Lock()
		for _, operation := range operations {
			if err := tracker.applyOperation(operation); err != nil {
				tracker.mutex.Unlock()
				return tracker.invalidate(ctx, err.Error())
			}
		}
		tracker.revision++
		snapshot := tracker.snapshotLocked()
		tracker.mutex.Unlock()
		return tracker.events.Publish(ctx, event.RunDiffUpdated{Revision: snapshot.Revision, Changes: eventChanges(snapshot.Changes)})
	case "execute_command", "write_stdin", "mcp_call":
		return tracker.invalidate(ctx, fmt.Sprintf("%s may have modified files outside exact Patch attribution", spec.Name))
	default:
		return nil
	}
}

func (tracker *Tracker) Snapshot() Snapshot {
	if tracker == nil {
		return Snapshot{}
	}
	tracker.mutex.RLock()
	defer tracker.mutex.RUnlock()
	return tracker.snapshotLocked()
}

type operation struct {
	Kind        string
	Path        string
	Destination string
	Bytes       int
	Created     bool
	Deleted     bool
	Moved       bool
}

func decodeOperations(value any) ([]operation, error) {
	if value == nil {
		return nil, errors.New("operations are missing")
	}
	values, ok := value.([]map[string]any)
	if !ok {
		generic, genericOK := value.([]any)
		if !genericOK {
			return nil, fmt.Errorf("operations have type %T", value)
		}
		values = make([]map[string]any, 0, len(generic))
		for index, item := range generic {
			entry, entryOK := item.(map[string]any)
			if !entryOK {
				return nil, fmt.Errorf("operation %d has type %T", index, item)
			}
			values = append(values, entry)
		}
	}
	result := make([]operation, 0, len(values))
	for index, value := range values {
		candidate := operation{
			Kind: stringValue(value["kind"]), Path: stringValue(value["path"]), Destination: stringValue(value["destination"]),
			Bytes: intValue(value["bytes"]), Created: boolValue(value["created"]), Deleted: boolValue(value["deleted"]), Moved: boolValue(value["moved"]),
		}
		if candidate.Path == "" {
			return nil, fmt.Errorf("operation %d has no path", index)
		}
		result = append(result, candidate)
	}
	return result, nil
}

func (tracker *Tracker) applyOperation(operation operation) error {
	path, err := tracker.canonical(operation.Path)
	if err != nil {
		return err
	}
	if operation.Moved {
		destination, err := tracker.canonical(operation.Destination)
		if err != nil {
			return err
		}
		current, exists := tracker.changes[path]
		delete(tracker.changes, path)
		switch {
		case exists && current.Kind == ChangeAdded:
			current.Path = destination
			current.Bytes = operation.Bytes
			tracker.changes[destination] = current
		case exists && current.Kind == ChangeMoved:
			current.Path = destination
			current.Bytes = operation.Bytes
			tracker.changes[destination] = current
		default:
			tracker.changes[destination] = Change{Path: destination, PreviousPath: path, Kind: ChangeMoved, Bytes: operation.Bytes}
		}
		return nil
	}
	current, exists := tracker.changes[path]
	switch {
	case operation.Created:
		tracker.changes[path] = Change{Path: path, Kind: ChangeAdded, Bytes: operation.Bytes}
	case operation.Deleted:
		if exists && current.Kind == ChangeAdded {
			delete(tracker.changes, path)
		} else if exists && current.Kind == ChangeMoved {
			delete(tracker.changes, path)
			tracker.changes[current.PreviousPath] = Change{Path: current.PreviousPath, Kind: ChangeDeleted}
		} else {
			tracker.changes[path] = Change{Path: path, Kind: ChangeDeleted}
		}
	default:
		if exists && (current.Kind == ChangeAdded || current.Kind == ChangeMoved) {
			current.Bytes = operation.Bytes
			tracker.changes[path] = current
		} else {
			tracker.changes[path] = Change{Path: path, Kind: ChangeUpdated, Bytes: operation.Bytes}
		}
	}
	return nil
}

func (tracker *Tracker) canonical(value string) (string, error) {
	value = filepath.Clean(strings.TrimSpace(value))
	if value == "" || value == "." {
		return "", fmt.Errorf("Run Diff path %q is invalid", value)
	}
	if filepath.IsAbs(value) {
		return value, nil
	}
	return filepath.Clean(filepath.Join(tracker.cwd, value)), nil
}

func (tracker *Tracker) invalidate(ctx context.Context, reason string) error {
	reason = strings.TrimSpace(reason)
	tracker.mutex.Lock()
	if tracker.invalidated {
		tracker.mutex.Unlock()
		return nil
	}
	tracker.invalidated = true
	tracker.reason = reason
	tracker.revision++
	revision := tracker.revision
	tracker.mutex.Unlock()
	return tracker.events.Publish(ctx, event.RunDiffInvalidated{Revision: revision, Reason: reason})
}

func (tracker *Tracker) snapshotLocked() Snapshot {
	changes := make([]Change, 0, len(tracker.changes))
	for _, change := range tracker.changes {
		changes = append(changes, change)
	}
	sort.Slice(changes, func(left, right int) bool { return changes[left].Path < changes[right].Path })
	return Snapshot{Changes: changes, Invalidated: tracker.invalidated, Reason: tracker.reason, Revision: tracker.revision}
}

func eventChanges(changes []Change) []event.RunDiffChange {
	result := make([]event.RunDiffChange, 0, len(changes))
	for _, change := range changes {
		result = append(result, event.RunDiffChange{Path: change.Path, PreviousPath: change.PreviousPath, Kind: string(change.Kind), Bytes: change.Bytes})
	}
	return result
}

func stringValue(value any) string {
	candidate, _ := value.(string)
	return strings.TrimSpace(candidate)
}
func boolValue(value any) bool { candidate, _ := value.(bool); return candidate }
func intValue(value any) int {
	switch candidate := value.(type) {
	case int:
		return candidate
	case int64:
		return int(candidate)
	case float64:
		return int(candidate)
	default:
		return 0
	}
}

var _ interface {
	After(context.Context, tool.Spec, tool.Call, tool.Result) error
} = (*Tracker)(nil)
