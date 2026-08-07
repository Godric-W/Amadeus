package react

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type PostExecutionHook interface {
	After(context.Context, tool.Spec, tool.Call, tool.Result) error
}

type PreExecutionHook interface {
	Before(context.Context, tool.Spec, tool.Call) error
}

type ToolExecutor struct {
	registry   *tool.Registry
	validator  *tool.ArgumentValidator
	authorizer tool.Authorizer
	events     event.Sink
	preHooks   []PreExecutionHook
	hooks      []PostExecutionHook
	now        func() time.Time
}

func NewToolExecutor(registry *tool.Registry, validator *tool.ArgumentValidator, authorizers ...tool.Authorizer) (*ToolExecutor, error) {
	options := ToolExecutorOptions{}
	if len(authorizers) > 0 {
		if authorizers[0] == nil {
			return nil, errors.New("tool executor authorizer is nil")
		}
		options.Authorizer = authorizers[0]
	}
	if len(authorizers) > 1 {
		return nil, errors.New("tool executor accepts at most one authorizer")
	}
	return NewToolExecutorWithOptions(registry, validator, options)
}

type ToolExecutorOptions struct {
	Authorizer tool.Authorizer
	Events     event.Sink
	PreHooks   []PreExecutionHook
	Hooks      []PostExecutionHook
}

func NewToolExecutorWithOptions(registry *tool.Registry, validator *tool.ArgumentValidator, options ToolExecutorOptions) (*ToolExecutor, error) {
	if registry == nil {
		return nil, errors.New("tool executor registry is nil")
	}
	if validator == nil {
		return nil, errors.New("tool executor argument validator is nil")
	}
	preHooks := append([]PreExecutionHook(nil), options.PreHooks...)
	for _, hook := range preHooks {
		if hook == nil {
			return nil, errors.New("tool executor pre-execution hook is nil")
		}
	}
	hooks := append([]PostExecutionHook(nil), options.Hooks...)
	for _, hook := range hooks {
		if hook == nil {
			return nil, errors.New("tool executor post-execution hook is nil")
		}
	}
	return &ToolExecutor{registry: registry, validator: validator, authorizer: options.Authorizer, events: options.Events, preHooks: preHooks, hooks: hooks, now: time.Now}, nil
}

func (executor *ToolExecutor) Execute(ctx context.Context, call tool.Call) (ToolOutcome, error) {
	startedAt := executor.now()
	if strings.TrimSpace(call.ID) == "" {
		return executor.failure(call, tool.Result{}, ToolOutcomeFailed, "invalid_call", errors.New("tool call ID is empty"), startedAt), nil
	}
	if strings.TrimSpace(call.Name) == "" {
		return executor.failure(call, tool.Result{}, ToolOutcomeFailed, "invalid_call", errors.New("tool call name is empty"), startedAt), nil
	}

	registered, ok := executor.registry.Lookup(call.Name)
	if !ok {
		return executor.failure(call, tool.Result{}, ToolOutcomeFailed, "not_registered", fmt.Errorf("tool %q is not registered", call.Name), startedAt), nil
	}
	spec := registered.Spec()
	normalized, err := executor.validator.Validate(spec, call.Arguments)
	if err != nil {
		return executor.failure(call, tool.Result{}, ToolOutcomeFailed, "invalid_arguments", err, startedAt), nil
	}
	normalizedCall := tool.NewCall(call.ID, call.Name, normalized)
	prepared, err := registered.Prepare(ctx, normalizedCall)
	if err != nil {
		status, kind := ToolOutcomeFailed, "prepare_failed"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status, kind = ToolOutcomeInterrupted, "interrupted"
		} else {
			var kindProvider tool.ErrorKindProvider
			if errors.As(err, &kindProvider) && strings.TrimSpace(kindProvider.ToolErrorKind()) != "" {
				kind = strings.TrimSpace(kindProvider.ToolErrorKind())
				if kind == "permission_required" || kind == "permission_denied" || kind == "path_denied" || kind == "symlink_escape" {
					status = ToolOutcomeDenied
				}
			}
		}
		outcome := executor.failure(call, tool.Result{}, status, kind, err, startedAt)
		return outcome, executor.publishCompleted(ctx, outcome)
	}
	preparedCall := prepared.Call()
	if preparedCall.ID != normalizedCall.ID || preparedCall.Name != normalizedCall.Name || string(preparedCall.Arguments) != string(normalizedCall.Arguments) {
		outcome := executor.failure(call, tool.Result{}, ToolOutcomeFailed, "invalid_prepared_call", errors.New("tool prepare changed normalized call identity or arguments"), startedAt)
		return outcome, executor.publishCompleted(ctx, outcome)
	}
	if executor.events != nil {
		presentation := tool.PresentCall(spec, preparedCall)
		if err := executor.events.Publish(ctx, event.ToolCallStarted{
			CallID: call.ID, ToolName: call.Name, SideEffect: string(spec.SideEffect),
			ActionSummary: presentation.ActionSummary, Detail: presentation.Detail,
		}); err != nil {
			return ToolOutcome{}, fmt.Errorf("publish tool call started: %w", err)
		}
	}
	if executor.authorizer != nil {
		if err := executor.authorizer.Authorize(ctx, spec, prepared); err != nil {
			kind := "approval_denied"
			var kindProvider tool.ErrorKindProvider
			if errors.As(err, &kindProvider) && strings.TrimSpace(kindProvider.ToolErrorKind()) != "" {
				kind = strings.TrimSpace(kindProvider.ToolErrorKind())
			}
			outcome := executor.failure(call, tool.Result{}, ToolOutcomeDenied, kind, err, startedAt)
			return outcome, executor.publishCompleted(ctx, outcome)
		}
	}
	for _, hook := range executor.preHooks {
		if err := hook.Before(ctx, spec, preparedCall); err != nil {
			outcome := executor.failure(call, tool.Result{}, ToolOutcomeFailed, "pre_hook_failed", err, startedAt)
			return outcome, executor.publishCompleted(ctx, outcome)
		}
	}

	result, executeErr := registered.Execute(ctx, prepared)
	result.CallID = call.ID
	result.ToolName = call.Name
	if executeErr != nil {
		status, kind := ToolOutcomeFailed, "execution_failed"
		var kindProvider tool.ErrorKindProvider
		if errors.As(executeErr, &kindProvider) && strings.TrimSpace(kindProvider.ToolErrorKind()) != "" {
			kind = strings.TrimSpace(kindProvider.ToolErrorKind())
		}
		if errors.Is(executeErr, context.Canceled) || errors.Is(executeErr, context.DeadlineExceeded) || ctx.Err() != nil {
			status, kind = ToolOutcomeInterrupted, "interrupted"
		}
		outcome := executor.failure(call, result, status, kind, executeErr, startedAt)
		executor.applyPostHooks(ctx, spec, preparedCall, result, &outcome)
		return outcome, executor.publishCompleted(ctx, outcome)
	}
	outcome := executor.success(call, result, startedAt)
	executor.applyPostHooks(ctx, spec, preparedCall, result, &outcome)
	return outcome, executor.publishCompleted(ctx, outcome)
}

func (executor *ToolExecutor) applyPostHooks(ctx context.Context, spec tool.Spec, call tool.Call, result tool.Result, outcome *ToolOutcome) {
	for _, hook := range executor.hooks {
		if hookErr := hook.After(ctx, spec, call, result.Clone()); hookErr != nil {
			if outcome.Metadata == nil {
				outcome.Metadata = make(map[string]any)
			}
			values, _ := outcome.Metadata["hook_errors"].([]string)
			outcome.Metadata["hook_errors"] = append(values, hookErr.Error())
		}
	}
}

func (executor *ToolExecutor) publishCompleted(ctx context.Context, outcome ToolOutcome) error {
	if executor.events == nil {
		return nil
	}
	completed := event.ToolCallCompleted{
		CallID: outcome.CallID, ToolName: outcome.ToolName,
		Success: outcome.Succeeded(), Partial: outcome.Partial,
		Summary: outcomeSummary(outcome), Duration: outcome.Duration,
	}
	if err := executor.events.Publish(ctx, completed); err != nil {
		return fmt.Errorf("publish tool call completed: %w", err)
	}
	return nil
}

func (executor *ToolExecutor) success(call tool.Call, result tool.Result, startedAt time.Time) ToolOutcome {
	return ToolOutcome{
		CallID: call.ID, ToolName: call.Name, Status: ToolOutcomeSucceeded,
		Result: result.Clone(), Partial: result.Partial, Duration: executor.durationSince(startedAt),
		Metadata: cloneMetadata(result.Metadata),
	}
}

func (executor *ToolExecutor) failure(call tool.Call, result tool.Result, status ToolOutcomeStatus, kind string, executionErr error, startedAt time.Time) ToolOutcome {
	if result.CallID == "" {
		result.CallID = call.ID
	}
	if result.ToolName == "" {
		result.ToolName = call.Name
	}
	return ToolOutcome{
		CallID: call.ID, ToolName: call.Name, Status: status, Result: result.Clone(),
		Error:    &ToolError{Kind: kind, Message: executionErr.Error()},
		Blocking: errors.Is(executionErr, project.ErrPathOutsideRoot), Partial: result.Partial,
		Duration: executor.durationSince(startedAt), Metadata: cloneMetadata(result.Metadata),
	}
}

func outcomeSummary(outcome ToolOutcome) string {
	if summary := strings.TrimSpace(outcome.Result.Text); summary != "" {
		return summary
	}
	if outcome.Error != nil && strings.TrimSpace(outcome.Error.Message) != "" {
		return outcome.Error.Message
	}
	if len(outcome.Result.Parts) != 0 {
		return fmt.Sprintf("tool returned %d content part(s)", len(outcome.Result.Parts))
	}
	if outcome.Partial {
		return "tool completed with partial output"
	}
	return "tool completed"
}

func cloneMetadata(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func (executor *ToolExecutor) durationSince(startedAt time.Time) time.Duration {
	finishedAt := executor.now()
	if finishedAt.Before(startedAt) {
		return 0
	}
	return finishedAt.Sub(startedAt)
}
