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
	mutex                  sync.RWMutex
	projects               map[ProjectID]Project
	projectByPath          map[string]ProjectID
	sessions               map[ConversationSessionID]ConversationSession
	turns                  map[TurnID]Turn
	messages               map[ConversationSessionID][]Message
	runs                   map[RunID]Run
	checkpoints            map[RunID][]Checkpoint
	checkpointIDs          map[CheckpointID]struct{}
	checkpointInstructions map[CheckpointID][]CheckpointInstruction
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		projects: make(map[ProjectID]Project), projectByPath: make(map[string]ProjectID),
		sessions: make(map[ConversationSessionID]ConversationSession), turns: make(map[TurnID]Turn),
		messages: make(map[ConversationSessionID][]Message), runs: make(map[RunID]Run),
		checkpoints: make(map[RunID][]Checkpoint), checkpointIDs: make(map[CheckpointID]struct{}),
		checkpointInstructions: make(map[CheckpointID][]CheckpointInstruction),
	}
}

func (store *MemoryStore) BeginFirstTurn(ctx context.Context, input BeginFirstTurnInput) (BeginTurnResult, error) {
	if err := validateStoreContext(ctx); err != nil {
		return BeginTurnResult{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := store.ensureInitialized(); err != nil {
		return BeginTurnResult{}, err
	}
	if err := store.ensureNewIDs(input.SessionID, input.TurnID, input.UserMessageID, input.RunID); err != nil {
		return BeginTurnResult{}, err
	}

	project, exists := store.projectForPath(input.CanonicalPath)
	if !exists {
		var err error
		project, err = NewProject(input.ProjectID, input.CanonicalPath, input.ProjectName, input.StartedAt)
		if err != nil {
			return BeginTurnResult{}, err
		}
		if _, duplicate := store.projects[project.ID]; duplicate {
			return BeginTurnResult{}, fmt.Errorf("%w: project ID %q", ErrConflict, project.ID)
		}
	} else {
		project.UpdatedAt = input.StartedAt.UTC()
		project.LastOpenedAt = input.StartedAt.UTC()
		if err := project.Validate(); err != nil {
			return BeginTurnResult{}, err
		}
	}

	conversation, err := NewConversationSession(input.SessionID, project.ID, input.SessionTitle, input.StartedAt)
	if err != nil {
		return BeginTurnResult{}, err
	}
	sequence, err := conversation.AllocateTurnSequence(input.StartedAt)
	if err != nil {
		return BeginTurnResult{}, err
	}
	turn, err := NewTurn(input.TurnID, conversation.ID, sequence, input.StartedAt)
	if err != nil {
		return BeginTurnResult{}, err
	}
	run, err := store.newRun(runInputFromFirst(input), conversation.ID, turn.ID)
	if err != nil {
		return BeginTurnResult{}, err
	}
	message, err := NewMessage(input.UserMessageID, conversation.ID, turn.ID, 1, MessageUser, input.UserContent, input.StartedAt)
	if err != nil {
		return BeginTurnResult{}, err
	}

	store.projects[project.ID] = project
	store.projectByPath[project.CanonicalPath] = project.ID
	store.sessions[conversation.ID] = conversation
	store.turns[turn.ID] = turn
	store.messages[conversation.ID] = []Message{message}
	store.runs[run.ID] = cloneRun(run)
	return cloneBeginResult(project, conversation, turn, message, run), nil
}

func (store *MemoryStore) BeginTurn(ctx context.Context, input BeginTurnInput) (BeginTurnResult, error) {
	if err := validateStoreContext(ctx); err != nil {
		return BeginTurnResult{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := store.ensureInitialized(); err != nil {
		return BeginTurnResult{}, err
	}
	if err := store.ensureNewIDs("", input.TurnID, input.UserMessageID, input.RunID); err != nil {
		return BeginTurnResult{}, err
	}
	conversation, ok := store.sessions[input.SessionID]
	if !ok {
		return BeginTurnResult{}, fmt.Errorf("%w: conversation session %q", ErrNotFound, input.SessionID)
	}
	if conversation.Status != ConversationSessionActive {
		return BeginTurnResult{}, fmt.Errorf("%w: conversation session %q is archived", ErrConflict, input.SessionID)
	}
	project, ok := store.projects[conversation.ProjectID]
	if !ok {
		return BeginTurnResult{}, fmt.Errorf("%w: project %q", ErrNotFound, conversation.ProjectID)
	}
	sequence, err := conversation.AllocateTurnSequence(input.StartedAt)
	if err != nil {
		return BeginTurnResult{}, err
	}
	turn, err := NewTurn(input.TurnID, conversation.ID, sequence, input.StartedAt)
	if err != nil {
		return BeginTurnResult{}, err
	}
	run, err := store.newRun(runInputFromNext(input), conversation.ID, turn.ID)
	if err != nil {
		return BeginTurnResult{}, err
	}
	messageSequence := int64(len(store.messages[conversation.ID]) + 1)
	message, err := NewMessage(input.UserMessageID, conversation.ID, turn.ID, messageSequence, MessageUser, input.UserContent, input.StartedAt)
	if err != nil {
		return BeginTurnResult{}, err
	}
	project.UpdatedAt = input.StartedAt.UTC()
	project.LastOpenedAt = input.StartedAt.UTC()
	if err := project.Validate(); err != nil {
		return BeginTurnResult{}, err
	}

	store.projects[project.ID] = project
	store.sessions[conversation.ID] = conversation
	store.turns[turn.ID] = turn
	store.messages[conversation.ID] = append(store.messages[conversation.ID], message)
	store.runs[run.ID] = cloneRun(run)
	return cloneBeginResult(project, conversation, turn, message, run), nil
}

func (store *MemoryStore) FinishTurn(ctx context.Context, input FinishTurnInput) (FinishTurnResult, error) {
	if err := validateStoreContext(ctx); err != nil {
		return FinishTurnResult{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := store.ensureInitialized(); err != nil {
		return FinishTurnResult{}, err
	}
	conversation, ok := store.sessions[input.SessionID]
	if !ok {
		return FinishTurnResult{}, fmt.Errorf("%w: conversation session %q", ErrNotFound, input.SessionID)
	}
	turn, ok := store.turns[input.TurnID]
	if !ok || turn.SessionID != conversation.ID {
		return FinishTurnResult{}, fmt.Errorf("%w: turn %q", ErrNotFound, input.TurnID)
	}
	run, ok := store.runs[input.RunID]
	if !ok || run.SessionID != conversation.ID || run.TurnID != turn.ID {
		return FinishTurnResult{}, fmt.Errorf("%w: run %q", ErrNotFound, input.RunID)
	}
	if !matchingTerminalStatuses(input.TurnStatus, input.RunStatus) {
		return FinishTurnResult{}, fmt.Errorf("%w: turn status %q does not match run status %q", ErrConflict, input.TurnStatus, input.RunStatus)
	}
	completed := input.TurnStatus == TurnCompleted
	if completed {
		if strings.TrimSpace(string(input.AssistantMessageID)) == "" || strings.TrimSpace(input.AssistantContent) == "" {
			return FinishTurnResult{}, errors.New("completed turn requires assistant message ID and content")
		}
		if _, duplicate := store.findMessage(input.AssistantMessageID); duplicate {
			return FinishTurnResult{}, fmt.Errorf("%w: message ID %q", ErrConflict, input.AssistantMessageID)
		}
	} else if input.AssistantMessageID != "" || strings.TrimSpace(input.AssistantContent) != "" {
		return FinishTurnResult{}, errors.New("non-completed turn cannot persist an assistant message")
	}

	run.UsageJSON = append([]byte(nil), input.UsageJSON...)
	if err := run.Finish(input.RunStatus, input.StopReason, input.FinishedAt); err != nil {
		return FinishTurnResult{}, err
	}
	if err := turn.Complete(input.TurnStatus, input.FinishedAt); err != nil {
		return FinishTurnResult{}, err
	}
	conversation.UpdatedAt = input.FinishedAt.UTC()
	conversation.LastActiveAt = input.FinishedAt.UTC()
	if err := conversation.Validate(); err != nil {
		return FinishTurnResult{}, err
	}

	var assistant *Message
	if completed {
		message, err := NewMessage(
			input.AssistantMessageID, conversation.ID, turn.ID,
			int64(len(store.messages[conversation.ID])+1), MessageAssistant, input.AssistantContent, input.FinishedAt,
		)
		if err != nil {
			return FinishTurnResult{}, err
		}
		assistant = &message
	}

	store.sessions[conversation.ID] = conversation
	store.turns[turn.ID] = turn
	store.runs[run.ID] = cloneRun(run)
	if assistant != nil {
		store.messages[conversation.ID] = append(store.messages[conversation.ID], *assistant)
	}
	return FinishTurnResult{Session: conversation, Turn: turn, Run: cloneRun(run), AssistantMessage: cloneMessagePointer(assistant)}, nil
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
		if run.SessionID != sessionID || run.Status != RunCancelled || run.FinishedAt == nil {
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

func (store *MemoryStore) AppendCheckpoint(ctx context.Context, input AppendCheckpointInput) (Checkpoint, error) {
	if err := validateStoreContext(ctx); err != nil {
		return Checkpoint{}, err
	}
	if err := input.Checkpoint.Validate(); err != nil {
		return Checkpoint{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	run, ok := store.runs[input.Checkpoint.RunID]
	if !ok {
		return Checkpoint{}, fmt.Errorf("%w: run %q", ErrNotFound, input.Checkpoint.RunID)
	}
	if _, duplicate := store.checkpointIDs[input.Checkpoint.ID]; duplicate {
		return Checkpoint{}, fmt.Errorf("%w: checkpoint ID %q", ErrConflict, input.Checkpoint.ID)
	}
	if input.Checkpoint.Sequence != run.LatestCheckpointSequence+1 {
		return Checkpoint{}, fmt.Errorf("%w: checkpoint sequence %d, expected %d", ErrConflict, input.Checkpoint.Sequence, run.LatestCheckpointSequence+1)
	}
	seenPrecedence := make(map[int]struct{}, len(input.Instructions))
	instructions := make([]CheckpointInstruction, len(input.Instructions))
	for index, instruction := range input.Instructions {
		if instruction.CheckpointID != input.Checkpoint.ID {
			return Checkpoint{}, errors.New("checkpoint instruction references a different checkpoint")
		}
		if err := instruction.Validate(); err != nil {
			return Checkpoint{}, err
		}
		if _, duplicate := seenPrecedence[instruction.Precedence]; duplicate {
			return Checkpoint{}, fmt.Errorf("%w: checkpoint instruction precedence %d", ErrConflict, instruction.Precedence)
		}
		seenPrecedence[instruction.Precedence] = struct{}{}
		instructions[index] = instruction
	}
	sort.Slice(instructions, func(left, right int) bool { return instructions[left].Precedence < instructions[right].Precedence })

	checkpoint := cloneCheckpoint(input.Checkpoint)
	run.LatestCheckpointSequence = checkpoint.Sequence
	store.runs[run.ID] = cloneRun(run)
	store.checkpoints[run.ID] = append(store.checkpoints[run.ID], checkpoint)
	store.checkpointIDs[checkpoint.ID] = struct{}{}
	store.checkpointInstructions[checkpoint.ID] = instructions
	return cloneCheckpoint(checkpoint), nil
}

func (store *MemoryStore) ListCheckpoints(ctx context.Context, runID RunID) ([]Checkpoint, error) {
	if err := validateStoreContext(ctx); err != nil {
		return nil, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if _, ok := store.runs[runID]; !ok {
		return nil, fmt.Errorf("%w: run %q", ErrNotFound, runID)
	}
	checkpoints := store.checkpoints[runID]
	result := make([]Checkpoint, len(checkpoints))
	for index, checkpoint := range checkpoints {
		result[index] = cloneCheckpoint(checkpoint)
	}
	return result, nil
}

func (store *MemoryStore) newRun(input runCreationInput, sessionID ConversationSessionID, turnID TurnID) (Run, error) {
	run, err := NewRun(input.ID, sessionID, turnID, input.Objective, input.StartedAt)
	if err != nil {
		return Run{}, err
	}
	run.ContextFromRunID = input.ContextFromRunID
	run.Provider = strings.TrimSpace(input.Provider)
	run.Model = strings.TrimSpace(input.Model)
	run.APIMode = strings.TrimSpace(input.APIMode)
	run.Dialect = strings.TrimSpace(input.Dialect)
	run.BudgetJSON = append([]byte(nil), input.BudgetJSON...)
	if err := run.Validate(); err != nil {
		return Run{}, err
	}
	if run.ContextFromRunID != "" {
		previous, ok := store.runs[run.ContextFromRunID]
		if !ok || previous.SessionID != sessionID || previous.Status != RunCancelled {
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
	BudgetJSON       []byte
	StartedAt        time.Time
}

func runInputFromFirst(input BeginFirstTurnInput) runCreationInput {
	return runCreationInput{
		ID: input.RunID, Objective: input.Objective, ContextFromRunID: input.ContextFromRunID,
		Provider: input.Provider, Model: input.Model, APIMode: input.APIMode, Dialect: input.Dialect,
		BudgetJSON: input.BudgetJSON, StartedAt: input.StartedAt,
	}
}

func runInputFromNext(input BeginTurnInput) runCreationInput {
	return runCreationInput{
		ID: input.RunID, Objective: input.Objective, ContextFromRunID: input.ContextFromRunID,
		Provider: input.Provider, Model: input.Model, APIMode: input.APIMode, Dialect: input.Dialect,
		BudgetJSON: input.BudgetJSON, StartedAt: input.StartedAt,
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

func (store *MemoryStore) ensureNewIDs(sessionID ConversationSessionID, turnID TurnID, messageID MessageID, runID RunID) error {
	if sessionID != "" {
		if _, duplicate := store.sessions[sessionID]; duplicate {
			return fmt.Errorf("%w: conversation session ID %q", ErrConflict, sessionID)
		}
	}
	if _, duplicate := store.turns[turnID]; duplicate {
		return fmt.Errorf("%w: turn ID %q", ErrConflict, turnID)
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
	if store == nil || store.projects == nil || store.sessions == nil || store.turns == nil || store.messages == nil || store.runs == nil {
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

func matchingTerminalStatuses(turn TurnStatus, run RunStatus) bool {
	return (turn == TurnCompleted && run == RunCompleted) ||
		(turn == TurnCancelled && run == RunCancelled) ||
		(turn == TurnFailed && run == RunFailed) ||
		(turn == TurnPartial && run == RunPartial) ||
		(turn == TurnNeedsPlan && run == RunNeedsPlan) ||
		(turn == TurnAwaitingUser && run == RunAwaitingUser)
}

func cloneBeginResult(project Project, conversation ConversationSession, turn Turn, message Message, run Run) BeginTurnResult {
	return BeginTurnResult{Project: project, Session: conversation, Turn: turn, Message: message, Run: cloneRun(run)}
}

func cloneRun(run Run) Run {
	run.BudgetJSON = append([]byte(nil), run.BudgetJSON...)
	run.UsageJSON = append([]byte(nil), run.UsageJSON...)
	if run.FinishedAt != nil {
		finishedAt := *run.FinishedAt
		run.FinishedAt = &finishedAt
	}
	return run
}

func cloneCheckpoint(checkpoint Checkpoint) Checkpoint {
	checkpoint.PayloadJSON = append([]byte(nil), checkpoint.PayloadJSON...)
	return checkpoint
}

func cloneMessagePointer(message *Message) *Message {
	if message == nil {
		return nil
	}
	cloned := *message
	return &cloned
}

var _ Store = (*MemoryStore)(nil)
