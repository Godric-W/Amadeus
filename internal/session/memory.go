package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mutex       sync.RWMutex
	projects    map[ProjectID]Project
	projectPath map[string]ProjectID
	sessions    map[SessionID]Session
	runs        map[RunID]Run
	items       map[SessionID][]RolloutItem
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		projects: make(map[ProjectID]Project), projectPath: make(map[string]ProjectID),
		sessions: make(map[SessionID]Session), runs: make(map[RunID]Run), items: make(map[SessionID][]RolloutItem),
	}
}

func (store *MemoryStore) GetProjectByCanonicalPath(ctx context.Context, path string) (Project, error) {
	if err := validateMemoryContext(ctx); err != nil {
		return Project{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	id, ok := store.projectPath[path]
	if !ok {
		return Project{}, ErrNotFound
	}
	return store.projects[id], nil
}

func (store *MemoryStore) GetSession(ctx context.Context, id SessionID) (Session, error) {
	if err := validateMemoryContext(ctx); err != nil {
		return Session{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	value, ok := store.sessions[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	return value, nil
}

func (store *MemoryStore) ListSessions(ctx context.Context, projectID ProjectID) ([]Session, error) {
	if err := validateMemoryContext(ctx); err != nil {
		return nil, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	result := make([]Session, 0)
	for _, value := range store.sessions {
		if value.ProjectID == projectID {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].LastActiveAt.Equal(result[j].LastActiveAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].LastActiveAt.After(result[j].LastActiveAt)
	})
	return result, nil
}

func (store *MemoryStore) LatestSession(ctx context.Context, projectID ProjectID) (Session, error) {
	values, err := store.ListSessions(ctx, projectID)
	if err != nil {
		return Session{}, err
	}
	for _, value := range values {
		if value.Status == SessionActive {
			return value, nil
		}
	}
	return Session{}, ErrNotFound
}

func (store *MemoryStore) RenameSession(ctx context.Context, input RenameSessionInput) (Session, error) {
	if err := validateMemoryContext(ctx); err != nil {
		return Session{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	value, ok := store.sessions[input.SessionID]
	if !ok {
		return Session{}, ErrNotFound
	}
	value.Title = strings.TrimSpace(input.Title)
	value.UpdatedAt = input.UpdatedAt.UTC()
	value.LastActiveAt = input.UpdatedAt.UTC()
	if err := value.Validate(); err != nil {
		return Session{}, err
	}
	store.sessions[value.ID] = value
	return value, nil
}

func (store *MemoryStore) DeleteSession(ctx context.Context, id SessionID) error {
	if err := validateMemoryContext(ctx); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if _, ok := store.sessions[id]; !ok {
		return ErrNotFound
	}
	for _, run := range store.runs {
		if run.SessionID == id && run.Status == RunRunning {
			return ErrConflict
		}
	}
	delete(store.sessions, id)
	delete(store.items, id)
	for runID, run := range store.runs {
		if run.SessionID == id {
			delete(store.runs, runID)
		}
	}
	return nil
}

func (store *MemoryStore) BeginFirstRun(ctx context.Context, input BeginFirstRunInput) (BeginRunResult, error) {
	if err := validateMemoryContext(ctx); err != nil {
		return BeginRunResult{}, err
	}
	payload, err := EncodePayload(UserMessagePayload{Content: strings.TrimSpace(input.UserContent)})
	if err != nil {
		return BeginRunResult{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	project, ok := store.projectByPathLocked(input.CanonicalPath)
	if !ok {
		project, err = NewProject(input.ProjectID, input.CanonicalPath, input.ProjectName, input.StartedAt)
		if err != nil {
			return BeginRunResult{}, err
		}
		if _, exists := store.projects[project.ID]; exists {
			return BeginRunResult{}, ErrConflict
		}
		store.projects[project.ID] = project
		store.projectPath[project.CanonicalPath] = project.ID
	}
	value, err := NewSession(input.SessionID, project.ID, input.SessionTitle, input.StartedAt)
	if err != nil {
		return BeginRunResult{}, err
	}
	run, err := NewRun(input.RunID, value.ID, 1, input.Mode, input.StartedAt)
	if err != nil {
		return BeginRunResult{}, err
	}
	applyMemoryMetadata(&run, input.Provider, input.Model, input.APIMode, input.Dialect)
	item, err := NewRolloutItem(input.UserItemID, value.ID, run.ID, 1, RolloutUserMessage, payload, input.StartedAt)
	if err != nil {
		return BeginRunResult{}, err
	}
	if _, exists := store.sessions[value.ID]; exists {
		return BeginRunResult{}, ErrConflict
	}
	value.NextRunSequence = 2
	value.NextItemSequence = 2
	store.sessions[value.ID] = value
	store.runs[run.ID] = run
	store.items[value.ID] = []RolloutItem{item}
	return BeginRunResult{Project: project, Session: value, Run: run, Item: item}, nil
}

func (store *MemoryStore) BeginRun(ctx context.Context, input BeginRunInput) (BeginRunResult, error) {
	if err := validateMemoryContext(ctx); err != nil {
		return BeginRunResult{}, err
	}
	payload, err := EncodePayload(UserMessagePayload{Content: strings.TrimSpace(input.UserContent)})
	if err != nil {
		return BeginRunResult{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	value, ok := store.sessions[input.SessionID]
	if !ok {
		return BeginRunResult{}, ErrNotFound
	}
	if value.Status != SessionActive || input.StartedAt.Before(value.LastActiveAt) {
		return BeginRunResult{}, ErrConflict
	}
	run, err := NewRun(input.RunID, value.ID, value.NextRunSequence, input.Mode, input.StartedAt)
	if err != nil {
		return BeginRunResult{}, err
	}
	applyMemoryMetadata(&run, input.Provider, input.Model, input.APIMode, input.Dialect)
	item, err := NewRolloutItem(input.UserItemID, value.ID, run.ID, value.NextItemSequence, RolloutUserMessage, payload, input.StartedAt)
	if err != nil {
		return BeginRunResult{}, err
	}
	if _, exists := store.runs[run.ID]; exists {
		return BeginRunResult{}, ErrConflict
	}
	value.NextRunSequence++
	value.NextItemSequence++
	value.UpdatedAt = input.StartedAt.UTC()
	value.LastActiveAt = input.StartedAt.UTC()
	store.sessions[value.ID] = value
	store.runs[run.ID] = run
	store.items[value.ID] = append(store.items[value.ID], item)
	return BeginRunResult{Project: store.projects[value.ProjectID], Session: value, Run: run, Item: item}, nil
}

func (store *MemoryStore) AppendItems(ctx context.Context, input AppendItemsInput) ([]RolloutItem, error) {
	if err := validateMemoryContext(ctx); err != nil {
		return nil, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.appendItemsLocked(input.SessionID, input.Items)
}

func (store *MemoryStore) FinishRun(ctx context.Context, input FinishRunInput) (FinishRunResult, error) {
	if err := validateMemoryContext(ctx); err != nil {
		return FinishRunResult{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	run, ok := store.runs[input.RunID]
	if !ok {
		return FinishRunResult{}, ErrNotFound
	}
	if run.SessionID != input.SessionID {
		return FinishRunResult{}, ErrConflict
	}
	items, err := store.appendItemsLocked(input.SessionID, input.TerminalItems)
	if err != nil {
		return FinishRunResult{}, err
	}
	if err := run.Finish(input.RunStatus, input.StopReason, input.UsageJSON, input.FinishedAt); err != nil {
		return FinishRunResult{}, err
	}
	store.runs[run.ID] = run
	value := store.sessions[input.SessionID]
	if input.FinishedAt.After(value.LastActiveAt) {
		value.UpdatedAt = input.FinishedAt.UTC()
		value.LastActiveAt = input.FinishedAt.UTC()
		store.sessions[value.ID] = value
	}
	return FinishRunResult{Session: value, Run: run, Items: items}, nil
}

func (store *MemoryStore) GetRun(ctx context.Context, id RunID) (Run, error) {
	if err := validateMemoryContext(ctx); err != nil {
		return Run{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	value, ok := store.runs[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	return cloneRun(value), nil
}

func (store *MemoryStore) ListItems(ctx context.Context, sessionID SessionID) ([]RolloutItem, error) {
	if err := validateMemoryContext(ctx); err != nil {
		return nil, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if _, ok := store.sessions[sessionID]; !ok {
		return nil, ErrNotFound
	}
	return cloneItems(store.items[sessionID]), nil
}

func (store *MemoryStore) RecoverRunningRuns(ctx context.Context, sessionID SessionID, at time.Time) error {
	if err := validateMemoryContext(ctx); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if _, ok := store.sessions[sessionID]; !ok {
		return ErrNotFound
	}
	for id, run := range store.runs {
		if run.SessionID != sessionID || run.Status != RunRunning {
			continue
		}
		pending, err := PendingToolCalls(store.items[sessionID], id)
		if err != nil {
			return err
		}
		drafts := make([]AppendItem, 0, len(pending)+1)
		activeCalls := make([]string, 0, len(pending))
		for _, call := range pending {
			draft, draftErr := InterruptedToolResultDraft(RecoveryItemID(id, call.ID), id, call, at, "process restarted before Tool Result was recorded", "process_terminated")
			if draftErr != nil {
				return draftErr
			}
			drafts = append(drafts, draft)
			activeCalls = append(activeCalls, call.ID)
		}
		payload, _ := EncodePayload(RunMarkerPayload{Reason: "process restarted", Guidance: "Re-plan from the current workspace state.", ActiveCalls: activeCalls})
		drafts = append(drafts, AppendItem{ID: RolloutItemID("recovered-" + string(id)), RunID: id, Kind: RolloutRunInterrupted, Payload: payload, CreatedAt: at})
		_, err = store.appendItemsLocked(sessionID, drafts)
		if err != nil {
			return err
		}
		if err := run.Finish(RunInterrupted, "process restarted", nil, at); err != nil {
			return err
		}
		store.runs[id] = run
	}
	return nil
}

func (store *MemoryStore) appendItemsLocked(sessionID SessionID, drafts []AppendItem) ([]RolloutItem, error) {
	value, ok := store.sessions[sessionID]
	if !ok {
		return nil, ErrNotFound
	}
	result := make([]RolloutItem, 0, len(drafts))
	sequence := value.NextItemSequence
	for _, draft := range drafts {
		if draft.RunID != "" {
			run, ok := store.runs[draft.RunID]
			if !ok || run.SessionID != sessionID {
				return nil, ErrConflict
			}
		}
		if draft.CreatedAt.Before(value.LastActiveAt) {
			return nil, errors.New("rollout item time precedes session activity")
		}
		item, err := NewRolloutItem(draft.ID, sessionID, draft.RunID, sequence, draft.Kind, draft.Payload, draft.CreatedAt)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
		sequence++
		value.UpdatedAt = draft.CreatedAt.UTC()
		value.LastActiveAt = draft.CreatedAt.UTC()
	}
	value.NextItemSequence = sequence
	store.sessions[sessionID] = value
	store.items[sessionID] = append(store.items[sessionID], result...)
	return cloneItems(result), nil
}

func (store *MemoryStore) projectByPathLocked(path string) (Project, bool) {
	id, ok := store.projectPath[path]
	if !ok {
		return Project{}, false
	}
	return store.projects[id], true
}
func validateMemoryContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("memory session context is nil")
	}
	return ctx.Err()
}
func applyMemoryMetadata(run *Run, provider, model, apiMode, dialect string) {
	run.Provider = strings.TrimSpace(provider)
	run.Model = strings.TrimSpace(model)
	run.APIMode = strings.TrimSpace(apiMode)
	run.Dialect = strings.TrimSpace(dialect)
}
func cloneRun(value Run) Run {
	value.UsageJSON = append(json.RawMessage(nil), value.UsageJSON...)
	if value.FinishedAt != nil {
		copy := *value.FinishedAt
		value.FinishedAt = &copy
	}
	return value
}
func cloneItems(values []RolloutItem) []RolloutItem {
	result := make([]RolloutItem, len(values))
	for index, value := range values {
		value.PayloadJSON = append(json.RawMessage(nil), value.PayloadJSON...)
		result[index] = value
	}
	return result
}

var _ Store = (*MemoryStore)(nil)
var _ = fmt.Sprintf
