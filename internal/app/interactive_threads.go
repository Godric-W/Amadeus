package app

import (
	"context"
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func (application *InteractiveApplication) LoadSessions(ctx context.Context) {
	values, err := application.workspace.List(ctx, threadstore.ListQuery{CWD: application.currentCWD()})
	current, _ := application.workspace.Current()
	options := make([]SessionOption, 0, len(values))
	for _, value := range values {
		options = append(options, SessionOption{ID: value.ID, Title: value.Title, Current: current != nil && value.ID == current.ID()})
	}
	application.emit(SessionsLoaded{Sessions: options, Error: err})
}

func (application *InteractiveApplication) Resume(ctx context.Context, id protocol.ThreadID) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	prepared, err := application.workspace.PrepareResume(ctx, id, application.configuration)
	if err != nil {
		application.emit(ThreadAttachFailed{Error: err})
		return
	}
	target := prepared.Target()
	generation := application.nextGeneration()
	snapshot, err := application.snapshot(ctx, target, generation)
	if err != nil {
		_ = prepared.Abort(context.WithoutCancel(ctx))
		application.emit(ThreadAttachFailed{Error: err})
		return
	}
	if err := prepared.Commit(); err != nil {
		_ = prepared.Abort(context.WithoutCancel(ctx))
		application.emit(ThreadAttachFailed{Error: err})
		return
	}
	previous := prepared.Previous()
	application.installAttachment(target, snapshot)
	application.emit(ThreadAttached{Snapshot: snapshot})
	if snapshot.Goal != nil && snapshot.Goal.Status == protocol.ThreadGoalActive {
		if err := target.EmitThreadIdle(ctx, extension.ThreadIdleCompleted); err != nil {
			application.emit(ApplicationError{Operation: "resume thread idle lifecycle", Error: err})
		}
	}
	application.releasePrevious(previous, target)
}

func (application *InteractiveApplication) Clear(ctx context.Context) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	prepared, err := application.workspace.PrepareNew(ctx, application.configuration)
	if err != nil {
		application.emit(ThreadAttachFailed{Error: err})
		return
	}
	target := prepared.Target()
	generation := application.nextGeneration()
	snapshot, err := application.snapshot(ctx, target, generation)
	if err != nil {
		_ = prepared.Abort(context.WithoutCancel(ctx))
		application.emit(ThreadAttachFailed{Error: err})
		return
	}
	if err := prepared.Commit(); err != nil {
		_ = prepared.Abort(context.WithoutCancel(ctx))
		application.emit(ThreadAttachFailed{Error: err})
		return
	}
	previous := prepared.Previous()
	application.installAttachment(target, snapshot)
	application.emit(ClearUIStarted{})
	application.emit(ThreadAttached{Snapshot: snapshot})
	application.releasePrevious(previous, target)
}

func (application *InteractiveApplication) Rename(ctx context.Context, generation uint64, name string) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		application.emit(ThreadRenameFailed{Error: errors.New("thread name cannot be empty")})
		return
	}
	active, currentGeneration, err := application.current()
	if err != nil {
		application.emit(ThreadRenameFailed{Error: err})
		return
	}
	if generation != currentGeneration {
		application.emit(ThreadRenameFailed{Error: ErrThreadSelectionChanged})
		return
	}
	if err := application.workspace.RenameCurrent(ctx, name); err != nil {
		application.emit(ThreadRenameFailed{Error: err})
		return
	}
	application.mu.Lock()
	application.title = name
	application.mu.Unlock()
	application.emit(ThreadNameUpdated{Generation: generation, ThreadID: active.ID(), Name: name})
}

func (application *InteractiveApplication) Delete(ctx context.Context, generation uint64) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	active, currentGeneration, err := application.current()
	if err != nil {
		application.emit(ThreadDeleteFailed{Error: err})
		return
	}
	if generation != currentGeneration {
		application.emit(ThreadDeleteFailed{Error: ErrThreadSelectionChanged})
		return
	}
	goal, _ := application.workspace.GetGoal(ctx, active.ID())
	application.stopAttachment()
	deleted, err := application.workspace.DeleteCurrent(ctx)
	if err != nil {
		application.installAttachment(active, application.currentSnapshot(active, generation))
		application.emit(ThreadDeleteFailed{Error: err})
		return
	}
	if goal != nil {
		cleanupGoalObjectiveFile(application.configuration.AmadeusRoot, goal.Objective)
	}
	application.emit(ThreadDeleted{ThreadID: deleted})
}
