package react

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type ToolExecution struct {
	Observation engine.Observation
	Evidence    engine.Evidence
}

type ToolExecutor struct {
	registry  *tool.Registry
	validator *tool.ArgumentValidator
	now       func() time.Time
}

func NewToolExecutor(registry *tool.Registry, validator *tool.ArgumentValidator) (*ToolExecutor, error) {
	if registry == nil {
		return nil, errors.New("tool executor registry is nil")
	}
	if validator == nil {
		return nil, errors.New("tool executor argument validator is nil")
	}
	return &ToolExecutor{registry: registry, validator: validator, now: time.Now}, nil
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

	result, executeErr := registered.Execute(ctx, normalized)
	result.CallID = call.ID
	result.ToolName = call.Name
	if executeErr != nil {
		return executor.failure(call, result, executeErr, startedAt), executeErr
	}
	return executor.success(call, result, startedAt), nil
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
