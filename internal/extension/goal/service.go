package goal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

type ObjectiveUpdate struct {
	Set   bool
	Value string
}

type SetRequest struct {
	ThreadID           protocol.ThreadID
	Objective          ObjectiveUpdate
	Status             *protocol.ThreadGoalStatus
	TokenBudget        state.TokenBudgetUpdate
	MaxGoalTokenBudget *int64
}

type previousGoal struct {
	GoalID    string
	Status    protocol.ThreadGoalStatus
	Objective string
}

type SetOutcome struct {
	Goal     protocol.ThreadGoal
	stored   state.ThreadGoal
	previous *previousGoal
	service  *Service
}

func (outcome SetOutcome) ApplyRuntimeEffects(ctx context.Context) error {
	if outcome.service == nil {
		return nil
	}
	runtime := outcome.service.runtime(outcome.Goal.ThreadID)
	if runtime == nil {
		return nil
	}
	return runtime.applyExternalSet(ctx, outcome.stored, outcome.previous)
}

func (outcome SetOutcome) PublishAndApply(ctx context.Context) error {
	if outcome.service == nil {
		return nil
	}
	runtime := outcome.service.runtime(outcome.Goal.ThreadID)
	if runtime != nil {
		event := protocol.ThreadGoalUpdatedEvent{ThreadID: outcome.Goal.ThreadID, Goal: outcome.Goal}
		if err := runtime.automation.PersistGoalUpdate(ctx, event); err != nil {
			runtime.events.Emit(protocol.Event{Msg: protocol.WarningEvent{ThreadID: outcome.Goal.ThreadID, Message: "persist goal rollout update: " + err.Error()}})
		}
		_ = outcome.service.state.Threads().SetPreviewIfEmpty(ctx, outcome.Goal.ThreadID, outcome.Goal.Objective, runtime.accounting.clock())
		if err := runtime.publishOrdered(ctx, protocol.Event{Msg: event}); err != nil {
			return err
		}
	}
	return outcome.ApplyRuntimeEffects(ctx)
}

type Service struct {
	state    state.Runtime
	mu       sync.Mutex
	runtimes map[protocol.ThreadID]*RuntimeHandle
}

func NewService(runtime state.Runtime) (*Service, error) {
	if runtime == nil || runtime.Goals() == nil || runtime.Threads() == nil {
		return nil, errors.New("goal service state runtime is unavailable")
	}
	return &Service{state: runtime, runtimes: make(map[protocol.ThreadID]*RuntimeHandle)}, nil
}

func (service *Service) register(runtime *RuntimeHandle) {
	if service == nil || runtime == nil {
		return
	}
	service.mu.Lock()
	service.runtimes[runtime.threadID] = runtime
	service.mu.Unlock()
}

func (service *Service) unregister(runtime *RuntimeHandle) {
	if service == nil || runtime == nil {
		return
	}
	service.mu.Lock()
	if service.runtimes[runtime.threadID] == runtime {
		delete(service.runtimes, runtime.threadID)
	}
	service.mu.Unlock()
}

func (service *Service) runtime(threadID protocol.ThreadID) *RuntimeHandle {
	if service == nil {
		return nil
	}
	service.mu.Lock()
	runtime := service.runtimes[threadID]
	service.mu.Unlock()
	return runtime
}

func (service *Service) Get(ctx context.Context, threadID protocol.ThreadID) (*protocol.ThreadGoal, error) {
	if service == nil {
		return nil, errors.New("goal service is nil")
	}
	if _, err := service.state.Threads().GetThread(ctx, threadID); err != nil && service.runtime(threadID) == nil {
		return nil, fmt.Errorf("read goal thread metadata: %w", err)
	}
	goal, err := service.state.Goals().Get(ctx, threadID)
	if err != nil || goal == nil {
		return nil, err
	}
	public := goal.Protocol()
	return &public, nil
}

func (service *Service) Set(ctx context.Context, request SetRequest) (SetOutcome, error) {
	if service == nil {
		return SetOutcome{}, errors.New("goal service is nil")
	}
	if request.ThreadID.IsZero() {
		return SetOutcome{}, errors.New("goal thread ID is empty")
	}
	if request.Objective.Set {
		request.Objective.Value = strings.TrimSpace(request.Objective.Value)
		if err := protocol.ValidateThreadGoalObjective(request.Objective.Value); err != nil {
			return SetOutcome{}, err
		}
	}
	if request.Status != nil && !request.Status.Valid() {
		return SetOutcome{}, errors.New("goal status is invalid")
	}
	if request.TokenBudget.Set {
		resolved, err := resolveBudget(request.TokenBudget.Value, request.MaxGoalTokenBudget)
		if err != nil {
			return SetOutcome{}, err
		}
		request.TokenBudget.Value = resolved
	}
	runtime := service.runtime(request.ThreadID)
	if err := service.ensureMutableThread(ctx, request.ThreadID, runtime); err != nil {
		return SetOutcome{}, err
	}
	var release func()
	var err error
	if runtime != nil {
		release, err = runtime.acquireGoalState(ctx)
		if err != nil {
			return SetOutcome{}, err
		}
		defer release()
		if err := runtime.prepareExternalMutation(ctx); err != nil {
			return SetOutcome{}, err
		}
	}
	existing, err := service.state.Goals().Get(ctx, request.ThreadID)
	if err != nil {
		return SetOutcome{}, err
	}
	var stored *state.ThreadGoal
	var previous *previousGoal
	if existing != nil {
		previous = &previousGoal{GoalID: existing.GoalID, Status: existing.Status, Objective: existing.Objective}
	}
	if request.Objective.Set {
		if existing == nil {
			status := protocol.ThreadGoalActive
			if request.Status != nil {
				status = *request.Status
			}
			budget := request.TokenBudget.Value
			if !request.TokenBudget.Set {
				budget = cloneInt64(request.MaxGoalTokenBudget)
			}
			created, createErr := service.state.Goals().Replace(ctx, request.ThreadID, request.Objective.Value, status, budget)
			if createErr != nil {
				return SetOutcome{}, createErr
			}
			stored = &created
		} else {
			stored, err = service.state.Goals().Update(ctx, request.ThreadID, state.GoalUpdate{
				Objective: &request.Objective.Value, Status: request.Status, TokenBudget: request.TokenBudget, ExpectedGoalID: existing.GoalID,
			})
		}
	} else {
		if existing == nil {
			return SetOutcome{}, errors.New("cannot update goal because this thread has no goal")
		}
		stored, err = service.state.Goals().Update(ctx, request.ThreadID, state.GoalUpdate{Status: request.Status, TokenBudget: request.TokenBudget, ExpectedGoalID: existing.GoalID})
	}
	if err != nil {
		return SetOutcome{}, err
	}
	if stored == nil {
		return SetOutcome{}, errors.New("goal changed concurrently")
	}
	_ = service.state.Threads().SetPreviewIfEmpty(ctx, request.ThreadID, stored.Objective, stored.UpdatedAt)
	return SetOutcome{Goal: stored.Protocol(), stored: *stored, previous: previous, service: service}, nil
}

func (service *Service) PublishCleared(ctx context.Context, threadID protocol.ThreadID) error {
	runtime := service.runtime(threadID)
	if runtime == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return runtime.publishOrdered(ctx, protocol.Event{Msg: protocol.ThreadGoalClearedEvent{ThreadID: threadID}})
}

func (service *Service) InheritSnapshotDeferred(ctx context.Context, sourceThreadID, targetThreadID protocol.ThreadID) (bool, error) {
	if service == nil || sourceThreadID.IsZero() || targetThreadID.IsZero() || sourceThreadID == targetThreadID {
		return false, errors.New("goal inheritance identity is invalid")
	}
	sourceRuntime := service.runtime(sourceThreadID)
	var release func()
	var err error
	if sourceRuntime != nil {
		release, err = sourceRuntime.acquireGoalState(ctx)
		if err != nil {
			return false, err
		}
		defer release()
		if err := sourceRuntime.prepareExternalMutation(ctx); err != nil {
			return false, err
		}
	}
	goal, err := service.state.Goals().Get(ctx, sourceThreadID)
	if err != nil || goal == nil {
		return false, err
	}
	goal.ThreadID = targetThreadID
	if err := service.state.Goals().ReplaceSnapshot(ctx, *goal); err != nil {
		return false, err
	}
	if targetRuntime := service.runtime(targetThreadID); targetRuntime != nil {
		if err := targetRuntime.restoreAfterResume(ctx); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (service *Service) Clear(ctx context.Context, threadID protocol.ThreadID) (bool, error) {
	if service == nil {
		return false, errors.New("goal service is nil")
	}
	runtime := service.runtime(threadID)
	if err := service.ensureMutableThread(ctx, threadID, runtime); err != nil {
		return false, err
	}
	var release func()
	var err error
	if runtime != nil {
		release, err = runtime.acquireGoalState(ctx)
		if err != nil {
			return false, err
		}
		defer release()
		if err := runtime.prepareExternalMutation(ctx); err != nil {
			return false, err
		}
	}
	cleared, err := service.state.Goals().Delete(ctx, threadID)
	if err != nil || cleared == nil {
		return false, err
	}
	if runtime != nil {
		runtime.accounting.clearActiveGoal()
	}
	return true, nil
}

func (service *Service) ensureMutableThread(ctx context.Context, threadID protocol.ThreadID, runtime *RuntimeHandle) error {
	metadata, err := service.state.Threads().GetThread(ctx, threadID)
	if err == nil {
		if metadata.Source.IsSubAgent() {
			return errors.New("sub-agent threads do not support direct goal mutation")
		}
		return nil
	}
	if !errors.Is(err, threadstore.ErrNotFound) {
		return fmt.Errorf("read goal thread metadata: %w", err)
	}
	if runtime != nil && runtime.toolsAvailable {
		return nil
	}
	return fmt.Errorf("goal thread is not persisted: %w", err)
}

func resolveBudget(value, maximum *int64) (*int64, error) {
	if value == nil {
		return cloneInt64(maximum), nil
	}
	if *value <= 0 {
		return nil, errors.New("goal token budget must be positive")
	}
	if maximum != nil && *value > *maximum {
		return nil, fmt.Errorf("goal token budget %d exceeds the maximum allowed goal token budget of %d", *value, *maximum)
	}
	return cloneInt64(value), nil
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
