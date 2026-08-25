package compact

import (
	"context"
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
)

const retainedUserMessageTokenBudget = int64(20_000)

type Service struct {
	ModelInfo     llm.ModelInfo
	ModelMessages llm.ModelMessages
}

func (service *Service) Generate(ctx context.Context, request Request) (Output, error) {
	if service == nil {
		return Output{}, errors.New("compaction service is unavailable")
	}
	if err := request.Validate(); err != nil {
		return Output{}, err
	}
	model := request.Model.Normalized()
	if strings.TrimSpace(model.Name) == "" {
		model = service.ModelInfo.Normalized()
	}
	messages := service.ModelMessages
	if model.ModelMessages.HasCompaction() {
		messages = model.ModelMessages
	}
	if !messages.HasCompaction() {
		return Output{}, errors.New("compaction model messages are unavailable")
	}
	compactionPrompt, summaryPrefix, err := internalprompt.CompactionMessages(messages.Normalized())
	if err != nil {
		return Output{}, err
	}
	prompt := request.Prompt
	prompt.Input = cloneItems(request.Source.PromptItems)
	prompt.Tools = nil
	prompt.ParallelToolCalls = false
	prompt.OutputSchema = nil
	prompt.OutputSchemaStrict = false
	var response llm.Response
	for {
		attemptPrompt := prompt
		attemptPrompt.Input = append(cloneItems(prompt.Input), llm.UserMessage(compactionPrompt.Text))
		response, err = request.ModelSession.Complete(ctx, engine.CompleteRequest{
			Request: llm.Request{
				Model: model.Name, InputModalities: append([]llm.InputModality(nil), model.InputModalities...),
				Prompt: attemptPrompt, Reasoning: request.Reasoning.Clone(), Metadata: request.Metadata,
			},
			Events: request.Events,
		})
		if err == nil {
			break
		}
		providerError, ok := llm.AsProviderError(err)
		if !ok || providerError.Kind != llm.ProviderErrorContextWindow {
			return Output{Message: response.Message, FinishReason: response.FinishReason, TokenUsage: response.TokenUsage}, err
		}
		trimmed, ok := trimOldestCompleteGroup(prompt.Input)
		if !ok {
			return Output{Message: response.Message, FinishReason: response.FinishReason, TokenUsage: response.TokenUsage}, err
		}
		prompt.Input = trimmed
	}
	estimator := request.Estimator
	if estimator == nil {
		estimator = agentcontext.ApproxTokenEstimator{}
	}
	replacement := buildReplacement(request.Source.UserMessages, summaryPrefix, response.Message.Content, estimator)
	return Output{
		Message: response.Message, FinishReason: response.FinishReason,
		ReplacementHistory: replacement, TokenUsage: response.TokenUsage,
	}, nil
}

func trimOldestCompleteGroup(items []llm.ResponseItem) ([]llm.ResponseItem, bool) {
	start := -1
	for index, item := range items {
		if item.Role != llm.RoleDeveloper && item.Role != llm.RoleSystem {
			start = index
			break
		}
	}
	if start < 0 || len(items)-start <= 1 {
		return nil, false
	}
	end := start + 1
	if len(items[start].ToolCalls) > 0 {
		pending := make(map[string]struct{}, len(items[start].ToolCalls))
		for _, call := range items[start].ToolCalls {
			pending[call.ID] = struct{}{}
		}
		for end < len(items) && len(pending) > 0 {
			item := items[end]
			if item.Role != llm.RoleTool {
				break
			}
			delete(pending, item.ToolCallID)
			end++
		}
	}
	trimmed := make([]llm.ResponseItem, 0, len(items)-(end-start))
	trimmed = append(trimmed, cloneItems(items[:start])...)
	trimmed = append(trimmed, cloneItems(items[end:])...)
	return trimmed, true
}

func buildReplacement(userMessages []llm.ResponseItem, summaryPrefix, summary string, estimator agentcontext.Estimator) []llm.ResponseItem {
	users := make([]llm.ResponseItem, 0, len(userMessages))
	for _, item := range userMessages {
		if strings.HasPrefix(strings.TrimSpace(item.Content), strings.TrimSpace(summaryPrefix)) {
			continue
		}
		users = append(users, cloneItems([]llm.ResponseItem{item})[0])
	}
	selected := make([]llm.ResponseItem, 0, len(users)+1)
	remaining := retainedUserMessageTokenBudget
	for index := len(users) - 1; index >= 0 && remaining > 0; index-- {
		item := users[index]
		cost := agentcontext.EstimateResponseItem(item, estimator)
		if cost > remaining {
			if len(item.Parts) > 0 {
				item.Parts = nil
				item.Content = strings.TrimSpace(item.Content) + "\n[Earlier user media omitted during compaction]"
			}
			item.Content = truncateText(item.Content, remaining, estimator)
			cost = agentcontext.EstimateResponseItem(item, estimator)
		}
		if strings.TrimSpace(item.Content) != "" {
			selected = append(selected, item)
			remaining -= min(cost, remaining)
		}
	}
	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
	}
	summaryText := strings.TrimSpace(summaryPrefix) + "\n" + strings.TrimSpace(summary)
	return append(selected, llm.UserMessage(summaryText))
}

func truncateText(value string, budget int64, estimator agentcontext.Estimator) string {
	runes := []rune(value)
	low, high := 0, len(runes)
	for low < high {
		middle := (low + high + 1) / 2
		if estimator.EstimateText(string(runes[:middle])) <= budget {
			low = middle
		} else {
			high = middle - 1
		}
	}
	return string(runes[:low])
}

func cloneItems(items []llm.ResponseItem) []llm.ResponseItem {
	cloned := make([]llm.ResponseItem, len(items))
	for index, item := range items {
		cloned[index] = item
		cloned[index].Parts = append([]llm.ContentPart(nil), item.Parts...)
		cloned[index].ToolCalls = append([]llm.ToolCall(nil), item.ToolCalls...)
		for callIndex := range cloned[index].ToolCalls {
			cloned[index].ToolCalls[callIndex].Arguments = append([]byte(nil), item.ToolCalls[callIndex].Arguments...)
		}
	}
	return cloned
}
