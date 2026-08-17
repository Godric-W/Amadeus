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

func (budget TurnBudget) Nearing(samples, toolCalls int, elapsed time.Duration) bool {
	ratio := budget.WarnRatio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.9
	}
	return budget.MaxSamples > 0 && float64(samples) >= float64(budget.MaxSamples)*ratio ||
		budget.MaxToolCalls > 0 && float64(toolCalls) >= float64(budget.MaxToolCalls)*ratio ||
		budget.MaxDuration > 0 && float64(elapsed) >= float64(budget.MaxDuration)*ratio
}

func (budget TurnBudget) Exhausted(samples, toolCalls int, elapsed time.Duration) string {
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

func PersistAssistantResponse(ctx context.Context, appendItems func(context.Context, turn.ID, ...rollout.Item) error, turnID turn.ID, message llm.Message, normalized []tool.ToolCall) error {
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
	return appendItems(ctx, turnID, items...)
}

func PublishModelCompletions(ctx context.Context, appendItems func(context.Context, turn.ID, ...rollout.Item) error, turnID turn.ID, events protocol.EventSink, sampleID string, message llm.Message) error {
	if strings.TrimSpace(message.Content) != "" {
		if err := persistAndPublishModelCompletion(ctx, appendItems, turnID, events, sampleID+":assistant", protocol.ItemAssistantMessage, message.Content); err != nil {
			return err
		}
	}
	if strings.TrimSpace(message.Reasoning) != "" {
		if err := persistAndPublishModelCompletion(ctx, appendItems, turnID, events, sampleID+":reasoning", protocol.ItemReasoning, message.Reasoning); err != nil {
			return err
		}
	}
	return nil
}

func persistAndPublishModelCompletion(ctx context.Context, appendItems func(context.Context, turn.ID, ...rollout.Item) error, turnID turn.ID, events protocol.EventSink, id string, kind protocol.ItemKind, text string) error {
	now := time.Now().UTC()
	turnItem := protocol.TurnItem{ID: id, Kind: kind, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: text}
	item, err := protocol.NewCompletedItem(turnItem)
	if err != nil {
		return err
	}
	completionCtx := context.WithoutCancel(ctx)
	if err := appendItems(completionCtx, turnID, item); err != nil {
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
