package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type IDFactory func(kind string) string
type Clock func() time.Time

type CoordinatorOptions struct {
	IDFactory IDFactory
	Clock     Clock
}

type RunMetadata struct {
	Provider   string
	Model      string
	APIMode    string
	Dialect    string
	BudgetJSON json.RawMessage
}

type StartedTurn struct {
	Records       BeginTurnResult
	PriorMessages []Message
	Interrupted   *Run
}

type Coordinator struct {
	store         Store
	canonicalPath string
	projectName   string
	idFactory     IDFactory
	clock         Clock
	current       ConversationSessionID
}

func NewCoordinator(store Store, canonicalPath, projectName string, options CoordinatorOptions) (*Coordinator, error) {
	if store == nil {
		return nil, errors.New("session coordinator store is nil")
	}
	canonicalPath = filepath.Clean(strings.TrimSpace(canonicalPath))
	if !filepath.IsAbs(canonicalPath) {
		return nil, errors.New("session coordinator project path must be absolute")
	}
	projectName = strings.TrimSpace(projectName)
	if projectName == "" {
		projectName = filepath.Base(canonicalPath)
	}
	if options.IDFactory == nil || options.Clock == nil {
		return nil, errors.New("session coordinator ID factory or clock is nil")
	}
	return &Coordinator{store: store, canonicalPath: canonicalPath, projectName: projectName, idFactory: options.IDFactory, clock: options.Clock}, nil
}

func (coordinator *Coordinator) CurrentSessionID() ConversationSessionID {
	if coordinator == nil {
		return ""
	}
	return coordinator.current
}

func (coordinator *Coordinator) ListSessions(ctx context.Context) ([]ConversationSession, error) {
	project, err := coordinator.store.GetProjectByCanonicalPath(ctx, coordinator.canonicalPath)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return []ConversationSession{}, nil
		}
		return nil, err
	}
	return coordinator.store.ListSessions(ctx, project.ID)
}

func (coordinator *Coordinator) Continue(ctx context.Context) (ConversationSession, error) {
	project, err := coordinator.store.GetProjectByCanonicalPath(ctx, coordinator.canonicalPath)
	if err != nil {
		return ConversationSession{}, err
	}
	conversation, err := coordinator.store.LatestSession(ctx, project.ID)
	if err != nil {
		return ConversationSession{}, err
	}
	coordinator.current = conversation.ID
	return conversation, nil
}

func (coordinator *Coordinator) Resume(ctx context.Context, id ConversationSessionID) (ConversationSession, error) {
	conversation, err := coordinator.store.GetSession(ctx, id)
	if err != nil {
		return ConversationSession{}, err
	}
	project, err := coordinator.store.GetProjectByCanonicalPath(ctx, coordinator.canonicalPath)
	if err != nil {
		return ConversationSession{}, err
	}
	if conversation.ProjectID != project.ID {
		return ConversationSession{}, fmt.Errorf("%w: session %q belongs to another project", ErrConflict, id)
	}
	coordinator.current = conversation.ID
	return conversation, nil
}

func (coordinator *Coordinator) BeginTask(ctx context.Context, objective string, metadata RunMetadata) (StartedTurn, error) {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return StartedTurn{}, errors.New("session task objective is empty")
	}
	now := coordinator.clock().UTC()
	turnID := TurnID(coordinator.nextID("turn"))
	messageID := MessageID(coordinator.nextID("msg"))
	runID := RunID(coordinator.nextID("run"))
	if coordinator.current == "" {
		result, err := coordinator.store.BeginFirstTurn(ctx, BeginFirstTurnInput{
			ProjectID: ProjectID(coordinator.nextID("project")), CanonicalPath: coordinator.canonicalPath, ProjectName: coordinator.projectName,
			SessionID: ConversationSessionID(coordinator.nextID("session")), SessionTitle: sessionTitle(objective), TurnID: turnID,
			UserMessageID: messageID, RunID: runID, Objective: objective, UserContent: objective,
			Provider: metadata.Provider, Model: metadata.Model, APIMode: metadata.APIMode, Dialect: metadata.Dialect,
			BudgetJSON: metadata.BudgetJSON, StartedAt: now,
		})
		if err != nil {
			return StartedTurn{}, err
		}
		coordinator.current = result.Session.ID
		return StartedTurn{Records: result}, nil
	}
	prior, err := coordinator.store.ListMessages(ctx, coordinator.current)
	if err != nil {
		return StartedTurn{}, err
	}
	var interrupted *Run
	contextRun, err := coordinator.store.PendingInterruptedRun(ctx, coordinator.current)
	if err == nil {
		interrupted = &contextRun
	} else if !errors.Is(err, ErrNotFound) {
		return StartedTurn{}, err
	}
	input := BeginTurnInput{
		SessionID: coordinator.current, TurnID: turnID, UserMessageID: messageID, RunID: runID,
		Objective: objective, UserContent: objective, Provider: metadata.Provider, Model: metadata.Model,
		APIMode: metadata.APIMode, Dialect: metadata.Dialect, BudgetJSON: metadata.BudgetJSON, StartedAt: now,
	}
	if interrupted != nil {
		input.ContextFromRunID = interrupted.ID
	}
	result, err := coordinator.store.BeginTurn(ctx, input)
	if err != nil {
		return StartedTurn{}, err
	}
	return StartedTurn{Records: result, PriorMessages: prior, Interrupted: interrupted}, nil
}

func (coordinator *Coordinator) FinishTask(ctx context.Context, started StartedTurn, status RunStatus, stopReason, assistantContent string, usage json.RawMessage) (FinishTurnResult, error) {
	turnStatus := TurnStatus(status)
	messageID := MessageID("")
	if status == RunCompleted {
		messageID = MessageID(coordinator.nextID("msg"))
	}
	return coordinator.store.FinishTurn(ctx, FinishTurnInput{
		SessionID: started.Records.Session.ID, TurnID: started.Records.Turn.ID, RunID: started.Records.Run.ID,
		TurnStatus: turnStatus, RunStatus: status, StopReason: strings.TrimSpace(stopReason),
		AssistantMessageID: messageID, AssistantContent: strings.TrimSpace(assistantContent), UsageJSON: usage,
		FinishedAt: coordinator.clock().UTC(),
	})
}

func (coordinator *Coordinator) AppendCheckpoint(ctx context.Context, input AppendCheckpointInput) (Checkpoint, error) {
	return coordinator.store.AppendCheckpoint(ctx, input)
}

func (coordinator *Coordinator) LatestCheckpoint(ctx context.Context, runID RunID) (Checkpoint, []CheckpointInstruction, error) {
	checkpoints, err := coordinator.store.ListCheckpoints(ctx, runID)
	if err != nil {
		return Checkpoint{}, nil, err
	}
	if len(checkpoints) == 0 {
		return Checkpoint{}, nil, fmt.Errorf("%w: checkpoint for run %q", ErrNotFound, runID)
	}
	checkpoint := checkpoints[len(checkpoints)-1]
	instructions, err := coordinator.store.ListCheckpointInstructions(ctx, checkpoint.ID)
	if err != nil {
		return Checkpoint{}, nil, err
	}
	return checkpoint, instructions, nil
}

func (coordinator *Coordinator) LatestSummary(ctx context.Context, sessionID ConversationSessionID) (ConversationSummary, error) {
	return coordinator.store.LatestSummary(ctx, sessionID)
}

func (coordinator *Coordinator) AppendSummary(ctx context.Context, summary ConversationSummary) (ConversationSummary, error) {
	return coordinator.store.AppendSummary(ctx, summary)
}

func (coordinator *Coordinator) nextID(kind string) string {
	return strings.TrimSpace(coordinator.idFactory(kind))
}

func sessionTitle(objective string) string {
	const maximum = 80
	title := strings.Join(strings.Fields(objective), " ")
	if len(title) <= maximum {
		return title
	}
	return title[:maximum-3] + "..."
}
