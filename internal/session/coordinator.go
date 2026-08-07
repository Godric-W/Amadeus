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
	Provider string
	Model    string
	APIMode  string
	Dialect  string
	Mode     RunMode
}

type StartedRun struct {
	Records    BeginRunResult
	PriorItems []RolloutItem
}

type Coordinator struct {
	store         Store
	canonicalPath string
	projectName   string
	idFactory     IDFactory
	clock         Clock
	current       SessionID
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

func (coordinator *Coordinator) CurrentSessionID() SessionID {
	if coordinator == nil {
		return ""
	}
	return coordinator.current
}

func (coordinator *Coordinator) ListSessions(ctx context.Context) ([]Session, error) {
	project, err := coordinator.store.GetProjectByCanonicalPath(ctx, coordinator.canonicalPath)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return []Session{}, nil
		}
		return nil, err
	}
	return coordinator.store.ListSessions(ctx, project.ID)
}

func (coordinator *Coordinator) Continue(ctx context.Context) (Session, error) {
	project, err := coordinator.store.GetProjectByCanonicalPath(ctx, coordinator.canonicalPath)
	if err != nil {
		return Session{}, err
	}
	value, err := coordinator.store.LatestSession(ctx, project.ID)
	if err != nil {
		return Session{}, err
	}
	coordinator.current = value.ID
	return value, nil
}

func (coordinator *Coordinator) Resume(ctx context.Context, id SessionID) (Session, error) {
	value, err := coordinator.store.GetSession(ctx, id)
	if err != nil {
		return Session{}, err
	}
	project, err := coordinator.store.GetProjectByCanonicalPath(ctx, coordinator.canonicalPath)
	if err != nil {
		return Session{}, err
	}
	if value.ProjectID != project.ID {
		return Session{}, fmt.Errorf("%w: session %q belongs to another project", ErrConflict, id)
	}
	coordinator.current = value.ID
	return value, nil
}

func (coordinator *Coordinator) BeginRun(ctx context.Context, objective string, metadata RunMetadata) (StartedRun, error) {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return StartedRun{}, errors.New("session run objective is empty")
	}
	if metadata.Mode == "" {
		metadata.Mode = RunModeExecute
	}
	if !metadata.Mode.Valid() {
		return StartedRun{}, fmt.Errorf("run mode %q is invalid", metadata.Mode)
	}
	now := coordinator.clock().UTC()
	itemID := RolloutItemID(coordinator.nextID("item"))
	runID := RunID(coordinator.nextID("run"))
	if coordinator.current == "" {
		result, err := coordinator.store.BeginFirstRun(ctx, BeginFirstRunInput{
			ProjectID: ProjectID(coordinator.nextID("project")), CanonicalPath: coordinator.canonicalPath, ProjectName: coordinator.projectName,
			SessionID: SessionID(coordinator.nextID("session")), SessionTitle: sessionTitle(objective),
			RunID: runID, UserItemID: itemID, UserContent: objective,
			Provider: metadata.Provider, Model: metadata.Model, APIMode: metadata.APIMode, Dialect: metadata.Dialect, Mode: metadata.Mode,
			StartedAt: now,
		})
		if err != nil {
			return StartedRun{}, err
		}
		coordinator.current = result.Session.ID
		return StartedRun{Records: result}, nil
	}
	if err := coordinator.store.RecoverRunningRuns(ctx, coordinator.current, now); err != nil {
		return StartedRun{}, err
	}
	prior, err := coordinator.store.ListItems(ctx, coordinator.current)
	if err != nil {
		return StartedRun{}, err
	}
	result, err := coordinator.store.BeginRun(ctx, BeginRunInput{
		SessionID: coordinator.current, RunID: runID, UserItemID: itemID, UserContent: objective,
		Provider: metadata.Provider, Model: metadata.Model, APIMode: metadata.APIMode, Dialect: metadata.Dialect, Mode: metadata.Mode,
		StartedAt: now,
	})
	if err != nil {
		return StartedRun{}, err
	}
	return StartedRun{Records: result, PriorItems: prior}, nil
}

func (coordinator *Coordinator) AppendItems(ctx context.Context, sessionID SessionID, drafts ...AppendItem) ([]RolloutItem, error) {
	return coordinator.store.AppendItems(ctx, AppendItemsInput{SessionID: sessionID, Items: drafts})
}

func (coordinator *Coordinator) FinishRun(ctx context.Context, started StartedRun, status RunStatus, stopReason, assistantContent string, usage json.RawMessage) (FinishRunResult, error) {
	now := coordinator.clock().UTC()
	items := make([]AppendItem, 0, 1)
	assistantContent = strings.TrimSpace(assistantContent)
	switch status {
	case RunCompleted:
		if assistantContent == "" {
			return FinishRunResult{}, errors.New("completed Run requires assistant content")
		}
		payload, err := EncodePayload(AssistantMessagePayload{Content: assistantContent})
		if err != nil {
			return FinishRunResult{}, err
		}
		items = append(items, AppendItem{ID: RolloutItemID(coordinator.nextID("item")), RunID: started.Records.Run.ID, Kind: RolloutAssistantMessage, Payload: payload, CreatedAt: now})
	case RunInterrupted, RunFailed:
		reason := strings.TrimSpace(stopReason)
		current, err := coordinator.store.ListItems(ctx, started.Records.Session.ID)
		if err != nil {
			return FinishRunResult{}, err
		}
		pending, err := PendingToolCalls(current, started.Records.Run.ID)
		if err != nil {
			return FinishRunResult{}, err
		}
		activeCalls := make([]string, 0, len(pending))
		for _, call := range pending {
			draft, draftErr := InterruptedToolResultDraft(RolloutItemID(coordinator.nextID("item")), started.Records.Run.ID, call, now, reason, "run_terminated")
			if draftErr != nil {
				return FinishRunResult{}, draftErr
			}
			items = append(items, draft)
			activeCalls = append(activeCalls, call.ID)
		}
		payload, err := EncodePayload(RunMarkerPayload{Reason: reason, Guidance: "Re-plan from the current workspace state.", ActiveCalls: activeCalls})
		if err != nil {
			return FinishRunResult{}, err
		}
		kind := RolloutRunInterrupted
		if status == RunFailed {
			kind = RolloutRunFailed
		}
		items = append(items, AppendItem{ID: RolloutItemID(coordinator.nextID("item")), RunID: started.Records.Run.ID, Kind: kind, Payload: payload, CreatedAt: now})
	default:
		return FinishRunResult{}, errors.New("FinishRun requires terminal status")
	}
	return coordinator.store.FinishRun(ctx, FinishRunInput{
		SessionID: started.Records.Session.ID, RunID: started.Records.Run.ID, RunStatus: status,
		StopReason: strings.TrimSpace(stopReason), UsageJSON: usage, TerminalItems: items, FinishedAt: now,
	})
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
