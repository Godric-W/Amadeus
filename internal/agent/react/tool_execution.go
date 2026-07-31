package react

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type ToolExecution struct {
	Observation engine.Observation
	Evidence    engine.Evidence
}

type ToolExecutor struct {
	registry   *tool.Registry
	validator  *tool.ArgumentValidator
	authorizer tool.Authorizer
	events     event.Sink
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
}

func NewToolExecutorWithOptions(registry *tool.Registry, validator *tool.ArgumentValidator, options ToolExecutorOptions) (*ToolExecutor, error) {
	if registry == nil {
		return nil, errors.New("tool executor registry is nil")
	}
	if validator == nil {
		return nil, errors.New("tool executor argument validator is nil")
	}
	return &ToolExecutor{registry: registry, validator: validator, authorizer: options.Authorizer, events: options.Events, now: time.Now}, nil
}

func (executor *ToolExecutor) Execute(ctx context.Context, call tool.Call) (ToolExecution, error) {
	startedAt := executor.now()
	if strings.TrimSpace(call.ID) == "" {
		err := errors.New("tool call ID is empty")
		return executor.failure(call, tool.Result{}, err, startedAt), err
	}
	if strings.TrimSpace(call.Name) == "" {
		err := errors.New("tool call name is empty")
		return executor.failure(call, tool.Result{}, err, startedAt), err
	}

	registered, ok := executor.registry.Lookup(call.Name)
	if !ok {
		err := fmt.Errorf("tool %q is not registered", call.Name)
		return executor.failure(call, tool.Result{}, err, startedAt), err
	}
	spec := registered.Spec()
	normalized, err := executor.validator.Validate(spec, call.Arguments)
	if err != nil {
		return executor.failure(call, tool.Result{}, err, startedAt), err
	}
	normalizedCall := tool.NewCall(call.ID, call.Name, normalized)
	if executor.events != nil {
		if err := executor.events.Publish(ctx, event.ToolCallStarted{CallID: call.ID, ToolName: call.Name}); err != nil {
			return executor.failure(call, tool.Result{}, err, startedAt), fmt.Errorf("publish tool call started: %w", err)
		}
	}
	if executor.authorizer != nil {
		if err := executor.authorizer.Authorize(ctx, spec, normalizedCall); err != nil {
			execution := executor.failure(call, tool.Result{}, err, startedAt)
			return execution, errors.Join(err, executor.publishCompleted(ctx, execution))
		}
	}

	result, executeErr := registered.Execute(ctx, normalized)
	result.CallID = call.ID
	result.ToolName = call.Name
	if executeErr != nil {
		execution := executor.failure(call, result, executeErr, startedAt)
		return execution, errors.Join(executeErr, executor.publishCompleted(ctx, execution))
	}
	execution := executor.success(call, result, startedAt)
	return execution, executor.publishCompleted(ctx, execution)
}

func (executor *ToolExecutor) publishCompleted(ctx context.Context, execution ToolExecution) error {
	if executor.events == nil {
		return nil
	}
	completed := event.ToolCallCompleted{
		CallID: execution.Observation.CallID, ToolName: execution.Observation.ToolName,
		Success: execution.Observation.Error == "", Partial: execution.Observation.Result.Partial,
		Summary: execution.Evidence.Summary, Duration: execution.Observation.Duration,
	}
	if err := executor.events.Publish(ctx, completed); err != nil {
		return fmt.Errorf("publish tool call completed: %w", err)
	}
	return nil
}

func (executor *ToolExecutor) success(call tool.Call, result tool.Result, startedAt time.Time) ToolExecution {
	observation := engine.Observation{
		CallID:   call.ID,
		ToolName: call.Name,
		Result:   result.Clone(),
		Duration: executor.durationSince(startedAt),
	}
	return ToolExecution{
		Observation: observation,
		Evidence: engine.Evidence{
			ID:       toolEvidenceID(call.ID),
			Kind:     engine.EvidenceTool,
			Source:   call.Name,
			Summary:  toolResultSummary(result),
			Verified: true,
		},
	}
}

func (executor *ToolExecutor) failure(call tool.Call, result tool.Result, executionErr error, startedAt time.Time) ToolExecution {
	if result.CallID == "" {
		result.CallID = call.ID
	}
	if result.ToolName == "" {
		result.ToolName = call.Name
	}
	message := executionErr.Error()
	return ToolExecution{
		Observation: engine.Observation{
			CallID:   call.ID,
			ToolName: call.Name,
			Result:   result.Clone(),
			Error:    message,
			Duration: executor.durationSince(startedAt),
		},
		Evidence: engine.Evidence{
			ID:       toolEvidenceID(call.ID),
			Kind:     engine.EvidenceTool,
			Source:   call.Name,
			Summary:  "tool failed: " + message,
			Verified: false,
		},
	}
}

func (executor *ToolExecutor) durationSince(startedAt time.Time) time.Duration {
	finishedAt := executor.now()
	if finishedAt.Before(startedAt) {
		return 0
	}
	return finishedAt.Sub(startedAt)
}

func toolEvidenceID(callID string) engine.EvidenceID {
	return engine.EvidenceID("tool:" + callID)
}

func toolResultSummary(result tool.Result) string {
	if summary := strings.TrimSpace(result.Text); summary != "" {
		return summary
	}
	if len(result.Parts) != 0 {
		return fmt.Sprintf("tool returned %d content part(s)", len(result.Parts))
	}
	if result.Partial {
		return "tool completed with partial output"
	}
	return "tool completed"
}
