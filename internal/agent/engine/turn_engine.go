package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type TurnHost interface {
	RolloutHost
	PromptSource
}

type CompactFunc func(context.Context) (bool, error)

type RunRequest struct {
	Host         TurnHost
	Turn         turn.Context
	Events       protocol.EventSink
	Instructions StepInstructionScope
	Compact      CompactFunc
}

type RunResult struct {
	Usage         llm.Usage
	ToolCallCount int
	Summary       string
	Outcome       rollout.TurnOutcome
	Reason        string
}

type TurnBudget struct {
	MaxSamples   int
	MaxToolCalls int
	MaxDuration  time.Duration
	WarnRatio    float64
}

func DefaultTurnBudget() TurnBudget {
	return TurnBudget{MaxSamples: 1_000, MaxToolCalls: 10_000, MaxDuration: 24 * time.Hour, WarnRatio: 0.9}
}

func (runtime *CodingRuntime) RunTurn(ctx context.Context, request RunRequest) (RunResult, error) {
	if runtime == nil || request.Host == nil || request.Events == nil || request.Instructions == nil {
		return RunResult{}, errors.New("turn engine request is incomplete")
	}
	var usage llm.Usage
	toolCallCount := 0
	startedAt := time.Now()
	completionReminderSent := false
	for stepNumber := 1; ; stepNumber++ {
		if err := ctx.Err(); err != nil {
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: cancelled", Outcome: rollout.TurnOutcomeAborted, Reason: err.Error()}, err
		}
		if reason := runtime.budget.exhausted(stepNumber-1, toolCallCount, time.Since(startedAt)); reason != "" {
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: blocked", Outcome: rollout.TurnOutcomeBlocked, Reason: reason}, nil
		}
		step, err := runtime.CaptureStep(request.Host, request.Events, request.Turn, request.Instructions)
		if err != nil {
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, err
		}
		if step.Prompt.NeedsCompaction(step.Model) && request.Compact != nil {
			compacted, compactErr := request.Compact(ctx)
			if compactErr != nil {
				return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, compactErr
			}
			if compacted {
				continue
			}
		}
		if !completionReminderSent && runtime.budget.nearing(stepNumber-1, toolCallCount, time.Since(startedAt)) {
			step.Prompt.Items = append(step.Prompt.Items, llm.DeveloperMessage("The Turn is approaching its internal safety budget. Finish the highest-value remaining work now and provide a concise final response; do not start optional work."))
			completionReminderSent = true
		}
		sampleID := fmt.Sprintf("%s/step-%d", request.Turn.TurnID, stepNumber)
		stepCtx := tool.WithRequestSnapshot(ctx, step.RequestSnapshot)
		stepCtx = tool.WithInvocationMetadata(stepCtx, tool.InvocationMetadata{SessionID: string(request.Turn.ThreadID), TurnID: string(request.Turn.TurnID), Source: tool.ToolCallSourceModel})
		sample, sampleErr := runtime.sampler.Sample(stepCtx, SampleRequest{
			ID: sampleID, Messages: step.Prompt.Items, BaseInstructions: step.BaseInstructions,
			Tools: step.Tools, OutputSchema: llm.OutputSchema(request.Turn.OutputSchema),
			Temperature: runtime.provider.Temperature, MaxOutputTokens: step.Model.MaxOutputTokens,
			Events: step.Events,
		})
		usage = addUsage(usage, sample.Response.Usage)
		if sampleErr != nil {
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, sampleErr
		}
		if sample.Kind == SampleFinal {
			if err := persistAssistantResponse(stepCtx, request.Host, request.Turn.TurnID, sample.Response.Message, nil); err != nil {
				return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, err
			}
			if err := publishModelCompletions(stepCtx, request.Host, request.Turn.TurnID, step.Events, sampleID, sample.Response.Message); err != nil {
				return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, err
			}
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: completed", Outcome: rollout.TurnOutcomeCompleted}, nil
		}
		toolCallCount += len(sample.ToolCalls)
		observer := newToolEventObserver(request.Host, request.Turn.TurnID, step.Events)
		recorded := false
		recorder := func(recordCtx context.Context, normalized []tool.ToolCall) error {
			recorded = true
			if err := persistAssistantResponse(recordCtx, request.Host, request.Turn.TurnID, sample.Response.Message, normalized); err != nil {
				return err
			}
			return publishModelCompletions(recordCtx, request.Host, request.Turn.TurnID, step.Events, sampleID, sample.Response.Message)
		}
		_, err = runtime.toolService.ExecuteBatchScoped(stepCtx, sample.ToolCalls, recorder, tool.ExecutionScope{
			Observer: observer, ContextScope: step.Instructions, AllowedTools: step.ToolNames,
		})
		if err != nil {
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, err
		}
		if !recorded {
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, errors.New("tool execution did not record model response")
		}
	}
}

func (budget TurnBudget) nearing(samples, toolCalls int, elapsed time.Duration) bool {
	ratio := budget.WarnRatio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.9
	}
	return budget.MaxSamples > 0 && float64(samples) >= float64(budget.MaxSamples)*ratio ||
		budget.MaxToolCalls > 0 && float64(toolCalls) >= float64(budget.MaxToolCalls)*ratio ||
		budget.MaxDuration > 0 && float64(elapsed) >= float64(budget.MaxDuration)*ratio
}

func (budget TurnBudget) exhausted(samples, toolCalls int, elapsed time.Duration) string {
	switch {
	case budget.MaxSamples > 0 && samples >= budget.MaxSamples:
		return fmt.Sprintf("internal model sample safety limit reached: %d", budget.MaxSamples)
	case budget.MaxToolCalls > 0 && toolCalls >= budget.MaxToolCalls:
		return fmt.Sprintf("internal tool call safety limit reached: %d", budget.MaxToolCalls)
	case budget.MaxDuration > 0 && elapsed >= budget.MaxDuration:
		return fmt.Sprintf("internal Turn duration safety limit reached: %s", budget.MaxDuration)
	default:
		return ""
	}
}

func persistAssistantResponse(ctx context.Context, host RolloutHost, turnID turn.ID, message llm.Message, normalized []tool.ToolCall) error {
	items := make([]rollout.Item, 0, len(normalized)+1)
	if strings.TrimSpace(message.Content) != "" || strings.TrimSpace(message.Reasoning) != "" {
		item, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseAssistantMessage, Role: string(llm.RoleAssistant), Content: strings.TrimSpace(message.Content), Reasoning: strings.TrimSpace(message.Reasoning)})
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	for _, call := range normalized {
		item, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseToolCall, Role: string(llm.RoleAssistant), CallID: call.ID, Name: call.Name, Arguments: append([]byte(nil), call.Payload...)})
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return errors.New("model response has no persistable items")
	}
	return host.AppendItems(ctx, turnID, items...)
}

func publishModelCompletions(ctx context.Context, host RolloutHost, turnID turn.ID, events protocol.EventSink, sampleID string, message llm.Message) error {
	if strings.TrimSpace(message.Content) != "" {
		if err := persistAndPublishModelCompletion(ctx, host, turnID, events, sampleID+":assistant", protocol.ItemAssistantMessage, message.Content); err != nil {
			return err
		}
	}
	if strings.TrimSpace(message.Reasoning) != "" {
		if err := persistAndPublishModelCompletion(ctx, host, turnID, events, sampleID+":reasoning", protocol.ItemReasoning, message.Reasoning); err != nil {
			return err
		}
	}
	return nil
}

func persistAndPublishModelCompletion(ctx context.Context, host RolloutHost, turnID turn.ID, events protocol.EventSink, id string, kind protocol.ItemKind, text string) error {
	now := time.Now().UTC()
	turnItem := protocol.TurnItem{ID: id, Kind: kind, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: text}
	item, err := protocol.NewCompletedItem(turnItem)
	if err != nil {
		return err
	}
	completionCtx := context.WithoutCancel(ctx)
	if err := host.AppendItems(completionCtx, turnID, item); err != nil {
		return fmt.Errorf("persist model completion: %w", err)
	}
	return events.Publish(completionCtx, protocol.SessionEvent{Message: protocol.ItemCompleted{Item: turnItem}})
}

func addUsage(total, next llm.Usage) llm.Usage {
	total.InputTokens += next.InputTokens
	total.CachedInputTokens += next.CachedInputTokens
	total.OutputTokens += next.OutputTokens
	total.ReasoningTokens += next.ReasoningTokens
	total.TotalTokens += next.TotalTokens
	return total
}

func UsageItem(usage llm.Usage) (rollout.Item, error) {
	return rollout.NewItem(rollout.KindTokenUsage, rollout.TokenUsage{
		InputTokens: usage.InputTokens, CachedInputTokens: usage.CachedInputTokens,
		OutputTokens: usage.OutputTokens, ReasoningTokens: usage.ReasoningTokens, TotalTokens: usage.TotalTokens,
	})
}
