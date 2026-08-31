package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/threadmanager"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func (application *InteractiveApplication) snapshot(ctx context.Context, active *threadmanager.AmadeusThread, generation uint64) (ThreadViewSnapshot, error) {
	history, err := active.History(ctx)
	if err != nil {
		return ThreadViewSnapshot{}, err
	}
	projection, err := ProjectRolloutItems(history)
	if err != nil {
		return ThreadViewSnapshot{}, err
	}
	tokenSnapshot := active.TokenCountSnapshot()
	title := "draft"
	configuration := active.Configuration()
	metadata, metadataErr := application.metadata(ctx, active.ID(), configuration.CWD)
	if metadataErr == nil {
		title = metadata.Title
	} else if !errors.Is(metadataErr, threadstore.ErrNotFound) {
		return ThreadViewSnapshot{}, metadataErr
	}
	return ThreadViewSnapshot{
		Generation: generation, SessionID: active.SessionID(), ThreadID: active.ID(), Title: title, Configuration: configuration,
		Items: projection.Items, TokenInfo: cloneTokenInfo(tokenSnapshot.Info),
		ActiveContextTokens: tokenSnapshot.ActiveContextTokens, ActiveContextEstimated: tokenSnapshot.ActiveContextEstimated,
	}, nil
}

func (application *InteractiveApplication) metadata(ctx context.Context, id protocol.ThreadID, currentDir string) (threadstore.StoredThread, error) {
	values, err := application.workspace.List(ctx, threadstore.ListQuery{CWD: currentDir, IncludeArchived: true})
	if err != nil {
		return threadstore.StoredThread{}, err
	}
	for _, value := range values {
		if value.ID == id {
			return value, nil
		}
	}
	return threadstore.StoredThread{}, threadstore.ErrNotFound
}

func (application *InteractiveApplication) nextGeneration() uint64 {
	application.mu.RLock()
	next := application.generation + 1
	application.mu.RUnlock()
	return next
}

func (application *InteractiveApplication) installAttachment(active *threadmanager.AmadeusThread, snapshot ThreadViewSnapshot) {
	application.mu.Lock()
	if application.attachmentCancel != nil {
		application.attachmentCancel()
	}
	ctx, cancel := context.WithCancel(application.ctx)
	application.active = active
	application.generation = snapshot.Generation
	application.attachmentCancel = cancel
	application.title = snapshot.Title
	application.phase = "idle"
	application.usage = protocol.TokenCountEvent{
		Info: cloneTokenInfo(snapshot.TokenInfo), ActiveContextTokens: snapshot.ActiveContextTokens,
		ActiveContextEstimated: snapshot.ActiveContextEstimated,
	}
	application.attachmentWG.Add(1)
	application.mu.Unlock()
	go func() {
		defer application.attachmentWG.Done()
		application.pumpAttachment(ctx, active, snapshot.Generation)
	}()
}

func (application *InteractiveApplication) currentSnapshot(active *threadmanager.AmadeusThread, generation uint64) ThreadViewSnapshot {
	application.mu.RLock()
	defer application.mu.RUnlock()
	return ThreadViewSnapshot{
		Generation: generation, SessionID: active.SessionID(), ThreadID: active.ID(), Title: application.title, Configuration: active.Configuration(),
		TokenInfo: cloneTokenInfo(application.usage.Info), ActiveContextTokens: application.usage.ActiveContextTokens,
		ActiveContextEstimated: application.usage.ActiveContextEstimated,
	}
}

func (application *InteractiveApplication) currentCWD() string {
	active, _, err := application.current()
	if err == nil {
		if currentDir := strings.TrimSpace(active.Configuration().CWD); currentDir != "" {
			return currentDir
		}
	}
	return application.configuration.CWD
}

func (application *InteractiveApplication) stopAttachment() {
	application.mu.Lock()
	if application.attachmentCancel != nil {
		application.attachmentCancel()
		application.attachmentCancel = nil
	}
	application.mu.Unlock()
}

func (application *InteractiveApplication) pumpAttachment(ctx context.Context, active *threadmanager.AmadeusThread, generation uint64) {
	io := active.Io()
	events := io.Events
	terminated := io.Terminated
	for events != nil || terminated != nil {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			application.observeSessionEvent(generation, event)
			application.emit(SessionEventObserved{Generation: generation, Event: event})
			if request, ok := event.Msg.(protocol.ApprovalRequestEvent); ok {
				var approval policy.ApprovalRequest
				if err := json.Unmarshal(request.Approval.Raw, &approval); err != nil {
					application.emit(ApplicationError{Operation: "decode approval request", Error: err})
					continue
				}
				application.emit(ApprovalRequested{Generation: generation, RequestID: string(request.RequestID), Request: approval})
			}
			if request, ok := event.Msg.(protocol.RequestUserInputEvent); ok {
				application.emit(UserInputRequested{Generation: generation, Request: request})
			}
		case _, ok := <-terminated:
			if !ok {
				terminated = nil
			}
		}
	}
}

func (application *InteractiveApplication) observeSessionEvent(generation uint64, event protocol.Event) {
	application.mu.Lock()
	defer application.mu.Unlock()
	if application.generation != generation || application.active == nil || application.active.ID() != protocol.ThreadIDOf(event.Msg) {
		return
	}
	switch message := event.Msg.(type) {
	case protocol.TurnStartedEvent:
		if application.phase != "compacting" {
			application.phase = "working"
		}
	case protocol.TurnCompleteEvent:
		application.phase = "completed"
	case protocol.TurnAbortedEvent:
		application.phase = "aborted"
	case protocol.ErrorEvent:
		application.phase = "idle"
	case protocol.TokenCountEvent:
		application.usage = message
	}
}

func (application *InteractiveApplication) current() (*threadmanager.AmadeusThread, uint64, error) {
	if application == nil {
		return nil, 0, errors.New("interactive application is nil")
	}
	application.mu.RLock()
	defer application.mu.RUnlock()
	if application.active == nil {
		return nil, application.generation, ErrNoActiveThread
	}
	return application.active, application.generation, nil
}

func (application *InteractiveApplication) releasePrevious(previous, current *threadmanager.AmadeusThread) {
	if previous == nil || previous == current {
		return
	}
	application.releaseWG.Add(1)
	go func() {
		defer application.releaseWG.Done()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(application.ctx), 5*time.Second)
		defer cancel()
		if err := application.workspace.Release(ctx, previous); err != nil {
			application.emit(ApplicationError{Operation: "release previous thread", Error: err})
		}
	}()
}

// waitForReleases gives attachment replacement workers an explicit owner and
// bounded shutdown point. The worker itself has the same bounded cleanup
// context, so a cancelled wait never creates an unbounded goroutine.
func (application *InteractiveApplication) waitForReleases(ctx context.Context) error {
	if application == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("attachment release wait context is nil")
	}
	done := make(chan struct{})
	go func() {
		application.releaseWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (application *InteractiveApplication) waitForAttachmentPumps(ctx context.Context) error {
	if application == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("attachment pump wait context is nil")
	}
	done := make(chan struct{})
	go func() {
		application.attachmentWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (application *InteractiveApplication) emit(event InteractiveEvent) {
	if application == nil || event == nil {
		return
	}
	select {
	case application.events <- event:
	case <-application.ctx.Done():
	}
}
