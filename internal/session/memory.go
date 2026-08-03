package session

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mutex         sync.RWMutex
	projects      map[ProjectID]Project
	projectByPath map[string]ProjectID
	sessions      map[ConversationSessionID]ConversationSession
	messages      map[ConversationSessionID][]Message
	runs          map[RunID]Run
	summaries     map[ConversationSessionID][]ConversationSummary
	summaryIDs    map[SummaryID]struct{}
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		projects: make(map[ProjectID]Project), projectByPath: make(map[string]ProjectID),
		sessions: make(map[ConversationSessionID]ConversationSession),
		messages: make(map[ConversationSessionID][]Message), runs: make(map[RunID]Run),
		summaries: make(map[ConversationSessionID][]ConversationSummary), summaryIDs: make(map[SummaryID]struct{}),
	}
}

func (store *MemoryStore) BeginFirstRun(ctx context.Context, input BeginFirstRunInput) (BeginRunResult, error) {
	if err := validateStoreContext(ctx); err != nil {
		return BeginRunResult{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := store.ensureInitialized(); err != nil {
		return BeginRunResult{}, err
	}
	if err := store.ensureNewIDs(input.SessionID, input.UserMessageID, input.RunID); err != nil {
		return BeginRunResult{}, err
	}

	project, exists := store.projectForPath(input.CanonicalPath)
	if !exists {
		var err error
		project, err = NewProject(input.ProjectID, input.CanonicalPath, input.ProjectName, input.StartedAt)
		if err != nil {
			return BeginRunResult{}, err
		}
		if _, duplicate := store.projects[project.ID]; duplicate {
			return BeginRunResult{}, fmt.Errorf("%w: project ID %q", ErrConflict, project.ID)
		}
	} else {
		project.UpdatedAt = input.StartedAt.UTC()
		project.LastOpenedAt = input.StartedAt.UTC()
		if err := project.Validate(); err != nil {
			return BeginRunResult{}, err
		}
	}

	conversation, err := NewConversationSession(input.SessionID, project.ID, input.SessionTitle, input.StartedAt)
	if err != nil {
		return BeginRunResult{}, err
	}
	sequence, err := conversation.AllocateRunSequence(input.StartedAt)
	if err != nil {
		return BeginRunResult{}, err
	}
	run, err := store.newRun(runInputFromFirst(input), conversation.ID, sequence)
	if err != nil {
		return BeginRunResult{}, err
	}
	message, err := NewMessage(input.UserMessageID, conversation.ID, run.ID, 1, MessageUser, input.UserContent, input.StartedAt)
	if err != nil {
		return BeginRunResult{}, err
	}

	store.projects[project.ID] = project
	store.projectByPath[project.CanonicalPath] = project.ID
	store.sessions[conversation.ID] = conversation
	store.messages[conversation.ID] = []Message{message}
	store.runs[run.ID] = cloneRun(run)
	return cloneBeginResult(project, conversation, message, run), nil
}

func (store *MemoryStore) BeginRun(ctx context.Context, input BeginRunInput) (BeginRunResult, error) {
	if err := validateStoreContext(ctx); err != nil {
		return BeginRunResult{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := store.ensureInitialized(); err != nil {
		return BeginRunResult{}, err
	}
	if err := store.ensureNewIDs("", input.UserMessageID, input.RunID); err != nil {
		return BeginRunResult{}, err
	}
	conversation, ok := store.sessions[input.SessionID]
	if !ok {
		return BeginRunResult{}, fmt.Errorf("%w: conversation session %q", ErrNotFound, input.SessionID)
	}
	if conversation.Status != ConversationSessionActive {
		return BeginRunResult{}, fmt.Errorf("%w: conversation session %q is archived", ErrConflict, input.SessionID)
	}
	project, ok := store.projects[conversation.ProjectID]
	if !ok {
		return BeginRunResult{}, fmt.Errorf("%w: project %q", ErrNotFound, conversation.ProjectID)
	}
	sequence, err := conversation.AllocateRunSequence(input.StartedAt)
	if err != nil {
		return BeginRunResult{}, err
	}
	run, err := store.newRun(runInputFromNext(input), conversation.ID, sequence)
	if err != nil {
		return BeginRunResult{}, err
	}
	messageSequence := int64(len(store.messages[conversation.ID]) + 1)
	message, err := NewMessage(input.UserMessageID, conversation.ID, run.ID, messageSequence, MessageUser, input.UserContent, input.StartedAt)
	if err != nil {
		return BeginRunResult{}, err
	}
	project.UpdatedAt = input.StartedAt.UTC()
	project.LastOpenedAt = input.StartedAt.UTC()
	if err := project.Validate(); err != nil {
		return BeginRunResult{}, err
	}

	store.projects[project.ID] = project
	store.sessions[conversation.ID] = conversation
	store.messages[conversation.ID] = append(store.messages[conversation.ID], message)
	store.runs[run.ID] = cloneRun(run)
	return cloneBeginResult(project, conversation, message, run), nil
}

func (store *MemoryStore) FinishRun(ctx context.Context, input FinishRunInput) (FinishRunResult, error) {
	if err := validateStoreContext(ctx); err != nil {
		return FinishRunResult{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := store.ensureInitialized(); err != nil {
		return FinishRunResult{}, err
	}
	conversation, ok := store.sessions[input.SessionID]
	if !ok {
		return FinishRunResult{}, fmt.Errorf("%w: conversation session %q", ErrNotFound, input.SessionID)
	}
	run, ok := store.runs[input.RunID]
	if !ok || run.SessionID != conversation.ID {
		return FinishRunResult{}, fmt.Errorf("%w: run %q", ErrNotFound, input.RunID)
	}
	completed := input.RunStatus == RunCompleted
	if completed {
		if strings.TrimSpace(string(input.AssistantMessageID)) == "" || strings.TrimSpace(input.AssistantContent) == "" {
			return FinishRunResult{}, errors.New("completed Run requires assistant message ID and content")
		}
		if _, duplicate := store.findMessage(input.AssistantMessageID); duplicate {
			return FinishRunResult{}, fmt.Errorf("%w: message ID %q", ErrConflict, input.AssistantMessageID)
		}
	} else if input.AssistantMessageID != "" || strings.TrimSpace(input.AssistantContent) != "" {
		return FinishRunResult{}, errors.New("non-completed Run cannot persist an assistant message")
	}

	run.UsageJSON = append([]byte(nil), input.UsageJSON...)
	run.InterruptedContextJSON = append([]byte(nil), input.InterruptedContext...)
	if err := run.Finish(input.RunStatus, input.StopReason, input.FinishedAt); err != nil {
		return FinishRunResult{}, err
	}
	conversation.UpdatedAt = input.FinishedAt.UTC()
	conversation.LastActiveAt = input.FinishedAt.UTC()
	if err := conversation.Validate(); err != nil {
		return FinishRunResult{}, err
	}

	var assistant *Message
	if completed {
		message, err := NewMessage(
			input.AssistantMessageID, conversation.ID, run.ID,
			int64(len(store.messages[conversation.ID])+1), MessageAssistant, input.AssistantContent, input.FinishedAt,
		)
		if err != nil {
			return FinishRunResult{}, err
		}
		assistant = &message
	}

	store.sessions[conversation.ID] = conversation
	store.runs[run.ID] = cloneRun(run)
	if assistant != nil {
		store.messages[conversation.ID] = append(store.messages[conversation.ID], *assistant)
	}
	return FinishRunResult{Session: conversation, Run: cloneRun(run), AssistantMessage: cloneMessagePointer(assistant)}, nil
}

func (store *MemoryStore) GetProjectByCanonicalPath(ctx context.Context, canonicalPath string) (Project, error) {
	if err := validateStoreContext(ctx); err != nil {
		return Project{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	project, ok := store.projectForPath(canonicalPath)
	if !ok {
		return Project{}, fmt.Errorf("%w: project path %q", ErrNotFound, canonicalPath)
	}
	return project, nil
}

func (store *MemoryStore) GetSession(ctx context.Context, id ConversationSessionID) (ConversationSession, error) {
	if err := validateStoreContext(ctx); err != nil {
		return ConversationSession{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	conversation, ok := store.sessions[id]
	if !ok {
		return ConversationSession{}, fmt.Errorf("%w: conversation session %q", ErrNotFound, id)
	}
	return conversation, nil
}

func (store *MemoryStore) ListSessions(ctx context.Context, projectID ProjectID) ([]ConversationSession, error) {
	if err := validateStoreContext(ctx); err != nil {
		return nil, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	result := make([]ConversationSession, 0)
	for _, conversation := range store.sessions {
		if conversation.ProjectID == projectID {
			result = append(result, conversation)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].LastActiveAt.Equal(result[right].LastActiveAt) {
			return result[left].ID < result[right].ID
		}
		return result[left].LastActiveAt.After(result[right].LastActiveAt)
	})
	return result, nil
}

func (store *MemoryStore) LatestSession(ctx context.Context, projectID ProjectID) (ConversationSession, error) {
	sessions, err := store.ListSessions(ctx, projectID)
	if err != nil {
		return ConversationSession{}, err
	}
	for _, conversation := range sessions {
		if conversation.Status == ConversationSessionActive {
			return conversation, nil
		}
	}
	return ConversationSession{}, fmt.Errorf("%w: active conversation session for project %q", ErrNotFound, projectID)
}

func (store *MemoryStore) ListMessages(ctx context.Context, sessionID ConversationSessionID) ([]Message, error) {
	if err := validateStoreContext(ctx); err != nil {
		return nil, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if _, ok := store.sessions[sessionID]; !ok {
		return nil, fmt.Errorf("%w: conversation session %q", ErrNotFound, sessionID)
	}
	return append([]Message(nil), store.messages[sessionID]...), nil
}

func (store *MemoryStore) GetRun(ctx context.Context, id RunID) (Run, error) {
	if err := validateStoreContext(ctx); err != nil {
		return Run{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	run, ok := store.runs[id]
	if !ok {
		return Run{}, fmt.Errorf("%w: run %q", ErrNotFound, id)
	}
	return cloneRun(run), nil
}

func (store *MemoryStore) LatestInterruptedRun(ctx context.Context, sessionID ConversationSessionID) (Run, error) {
	if err := validateStoreContext(ctx); err != nil {
		return Run{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	var latest Run
	found := false
	for _, run := range store.runs {
		if run.SessionID != sessionID || (run.Status != RunInterrupted && run.Status != RunFailed) || run.FinishedAt == nil {
			continue
		}
		if !found || run.FinishedAt.After(*latest.FinishedAt) || (run.FinishedAt.Equal(*latest.FinishedAt) && run.ID < latest.ID) {
			latest = run
			found = true
		}
	}
	if !found {
		return Run{}, fmt.Errorf("%w: interrupted run for conversation session %q", ErrNotFound, sessionID)
	}
	return cloneRun(latest), nil
}

func (store *MemoryStore) PendingInterruptedRun(ctx context.Context, sessionID ConversationSessionID) (Run, error) {
	if err := validateStoreContext(ctx); err != nil {
		return Run{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	var latestCancelled Run
	var latestCompleted time.Time
	for _, run := range store.runs {
		if run.SessionID != sessionID || run.FinishedAt == nil {
			continue
		}
		if run.Status == RunCompleted && run.FinishedAt.After(latestCompleted) {
			latestCompleted = *run.FinishedAt
		}
		if (run.Status == RunInterrupted || run.Status == RunFailed) && len(run.InterruptedContextJSON) != 0 && (latestCancelled.FinishedAt == nil || run.FinishedAt.After(*latestCancelled.FinishedAt)) {
			latestCancelled = run
		}
	}
	if latestCancelled.FinishedAt == nil || !latestCancelled.FinishedAt.After(latestCompleted) {
		return Run{}, fmt.Errorf("%w: pending interrupted run for conversation session %q", ErrNotFound, sessionID)
	}
	return cloneRun(latestCancelled), nil
}

func (store *MemoryStore) RecoverRunningRuns(ctx context.Context, sessionID ConversationSessionID, at time.Time) error {
	if err := validateStoreContext(ctx); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := store.ensureInitialized(); err != nil {
		return err
	}
	conversation, ok := store.sessions[sessionID]
	if !ok {
		return fmt.Errorf("%w: conversation session %q", ErrNotFound, sessionID)
	}
	changed := false
	latest := at.UTC()
	for id, run := range store.runs {
		if run.SessionID != sessionID || run.Status != RunRunning {
			continue
		}
		finishedAt := latest
		if finishedAt.Before(run.StartedAt) {
			finishedAt = run.StartedAt
		}
		contextJSON, err := EncodeInterruptedContext(InterruptedContextV1{
			Objective: run.Objective, Status: string(RunInterrupted), StopReason: "previous process ended before run completion",
			LastError: "previous process ended before run completion", PendingWork: []string{"Re-plan from the current workspace state."},
		})
		if err != nil {
			return err
		}
		run.InterruptedContextJSON = contextJSON
		if err := run.Finish(RunInterrupted, "previous process ended before run completion", finishedAt); err != nil {
			return err
		}
		store.runs[id] = cloneRun(run)
		changed = true
		if run.FinishedAt != nil && run.FinishedAt.After(latest) {
			latest = *run.FinishedAt
		}
	}
	if !changed {
		return nil
	}
	if latest.Before(conversation.UpdatedAt) {
		latest = conversation.UpdatedAt
	}
	conversation.UpdatedAt = latest
	conversation.LastActiveAt = latest
	if err := conversation.Validate(); err != nil {
		return err
	}
	store.sessions[sessionID] = conversation
	return nil
}

func (store *MemoryStore) AppendSummary(ctx context.Context, summary ConversationSummary) (ConversationSummary, error) {
	if err := validateStoreContext(ctx); err != nil {
		return ConversationSummary{}, err
	}
	if err := summary.Validate(); err != nil {
		return ConversationSummary{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if _, ok := store.sessions[summary.SessionID]; !ok {
		return ConversationSummary{}, fmt.Errorf("%w: conversation session %q", ErrNotFound, summary.SessionID)
	}
	if _, duplicate := store.summaryIDs[summary.ID]; duplicate {
		return ConversationSummary{}, fmt.Errorf("%w: conversation summary %q", ErrConflict, summary.ID)
	}
	for _, existing := range store.summaries[summary.SessionID] {
		if existing.FromMessageSequence == summary.FromMessageSequence && existing.ToMessageSequence == summary.ToMessageSequence && existing.SourceHash == summary.SourceHash {
			return existing, nil
		}
	}
	store.summaries[summary.SessionID] = append(store.summaries[summary.SessionID], summary)
	store.summaryIDs[summary.ID] = struct{}{}
	return summary, nil
}

func (store *MemoryStore) LatestSummary(ctx context.Context, sessionID ConversationSessionID) (ConversationSummary, error) {
	if err := validateStoreContext(ctx); err != nil {
		return ConversationSummary{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	items := store.summaries[sessionID]
	if len(items) == 0 {
		return ConversationSummary{}, fmt.Errorf("%w: conversation summary for session %q", ErrNotFound, sessionID)
	}
	latest := items[0]
	for _, item := range items[1:] {
		if item.ToMessageSequence > latest.ToMessageSequence || (item.ToMessageSequence == latest.ToMessageSequence && item.CreatedAt.After(latest.CreatedAt)) {
			latest = item
		}
	}
	return latest, nil
}

func (store *MemoryStore) newRun(input runCreationInput, sessionID ConversationSessionID, sequence int64) (Run, error) {
	run, err := NewSequencedRun(input.ID, sessionID, sequence, input.Objective, input.StartedAt)
	if err != nil {
		return Run{}, err
	}
	run.ContextFromRunID = input.ContextFromRunID
	run.Provider = strings.TrimSpace(input.Provider)
	run.Model = strings.TrimSpace(input.Model)
	run.APIMode = strings.TrimSpace(input.APIMode)
	run.Dialect = strings.TrimSpace(input.Dialect)
	if input.ExecutionMode.Valid() {
		run.ExecutionMode = input.ExecutionMode
	}
	if err := run.Validate(); err != nil {
		return Run{}, err
	}
	if run.ContextFromRunID != "" {
		previous, ok := store.runs[run.ContextFromRunID]
		if !ok || previous.SessionID != sessionID || (previous.Status != RunInterrupted && previous.Status != RunFailed) {
			return Run{}, fmt.Errorf("%w: interrupted context run %q", ErrConflict, run.ContextFromRunID)
		}
	}
	return run, nil
}

type runCreationInput struct {
	ID               RunID
	Objective        string
	ContextFromRunID RunID
	Provider         string
	Model            string
	APIMode          string
	Dialect          string
	ExecutionMode    ExecutionMode
	StartedAt        time.Time
}

func runInputFromFirst(input BeginFirstRunInput) runCreationInput {
	return runCreationInput{
		ID: input.RunID, Objective: input.Objective, ContextFromRunID: input.ContextFromRunID,
		Provider: input.Provider, Model: input.Model, APIMode: input.APIMode, Dialect: input.Dialect, ExecutionMode: input.ExecutionMode,
		StartedAt: input.StartedAt,
	}
}

func runInputFromNext(input BeginRunInput) runCreationInput {
	return runCreationInput{
		ID: input.RunID, Objective: input.Objective, ContextFromRunID: input.ContextFromRunID,
		Provider: input.Provider, Model: input.Model, APIMode: input.APIMode, Dialect: input.Dialect, ExecutionMode: input.ExecutionMode,
		StartedAt: input.StartedAt,
	}
}

func (store *MemoryStore) projectForPath(canonicalPath string) (Project, bool) {
	id, ok := store.projectByPath[strings.TrimSpace(canonicalPath)]
	if !ok {
		return Project{}, false
	}
	project, ok := store.projects[id]
	return project, ok
}

func (store *MemoryStore) ensureNewIDs(sessionID ConversationSessionID, messageID MessageID, runID RunID) error {
	if sessionID != "" {
		if _, duplicate := store.sessions[sessionID]; duplicate {
			return fmt.Errorf("%w: conversation session ID %q", ErrConflict, sessionID)
		}
	}
	if _, duplicate := store.runs[runID]; duplicate {
		return fmt.Errorf("%w: run ID %q", ErrConflict, runID)
	}
	if _, duplicate := store.findMessage(messageID); duplicate {
		return fmt.Errorf("%w: message ID %q", ErrConflict, messageID)
	}
	return nil
}

func (store *MemoryStore) findMessage(id MessageID) (Message, bool) {
	for _, messages := range store.messages {
		for _, message := range messages {
			if message.ID == id {
				return message, true
			}
		}
	}
	return Message{}, false
}

func (store *MemoryStore) ensureInitialized() error {
	if store == nil || store.projects == nil || store.sessions == nil || store.messages == nil || store.runs == nil || store.summaries == nil || store.summaryIDs == nil {
		return errors.New("session memory store is nil or uninitialized")
	}
	return nil
}

func validateStoreContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("session store context is nil")
	}
	return ctx.Err()
}

func cloneBeginResult(project Project, conversation ConversationSession, message Message, run Run) BeginRunResult {
	return BeginRunResult{Project: project, Session: conversation, Message: message, Run: cloneRun(run)}
}

func cloneRun(run Run) Run {
	run.UsageJSON = append([]byte(nil), run.UsageJSON...)
	run.InterruptedContextJSON = append([]byte(nil), run.InterruptedContextJSON...)
	if run.FinishedAt != nil {
		finishedAt := *run.FinishedAt
		run.FinishedAt = &finishedAt
	}
	return run
}

func cloneMessagePointer(message *Message) *Message {
	if message == nil {
		return nil
	}
	cloned := *message
	return &cloned
}

var _ Store = (*MemoryStore)(nil)
