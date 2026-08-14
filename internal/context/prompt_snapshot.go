package agentcontext

import (
	"encoding/json"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
)

func (manager *Manager) Snapshot(model llm.ModelInfo, prompt llm.Prompt) PromptSnapshot {
	if manager == nil {
		return PromptSnapshot{}
	}
	manager.mu.RLock()
	items := cloneResponseItems(manager.items)
	updates := make(map[UpdateKey]string, len(manager.updates))
	for key, value := range manager.updates {
		updates[key] = value
	}
	version := manager.historyVersion
	usage := UsageSnapshot{ProviderUsage: manager.providerUsage, HasProviderUsage: manager.hasUsage}
	estimator := manager.estimator
	manager.mu.RUnlock()

	model = model.Normalized()
	normalized := normalizeHistory(items, model, estimator)
	result := make([]llm.ResponseItem, 0, len(normalized)+len(updates))
	for _, key := range updateOrder {
		if content := strings.TrimSpace(updates[key]); content != "" {
			result = append(result, llm.DeveloperMessage(content))
		}
	}
	result = append(result, normalized...)
	usage.EstimatedInputTokens = estimateResponseItems(result, estimator) + estimatePromptOverhead(prompt, estimator)
	return PromptSnapshot{Items: result, Usage: usage, HistoryVersion: version}
}

func (snapshot PromptSnapshot) NeedsCompaction(model llm.ModelInfo) bool {
	model = model.Normalized()
	if model.AutoCompactTokenLimit > 0 && snapshot.Usage.EstimatedInputTokens >= model.AutoCompactTokenLimit {
		return true
	}
	return model.ContextWindow > 0 && snapshot.Usage.EstimatedInputTokens+int64(model.MaxOutputTokens) >= model.ContextWindow
}

func estimatePromptOverhead(prompt llm.Prompt, estimator Estimator) int64 {
	estimated := estimateResponseItems(prompt.Input, estimator)
	estimated += estimator.EstimateText(prompt.BaseInstructions.Text)
	if encoded, err := json.Marshal(prompt.Tools); err == nil {
		estimated += estimator.EstimateText(string(encoded))
	}
	if len(prompt.OutputSchema) > 0 {
		estimated += estimator.EstimateText(string(prompt.OutputSchema))
	}
	return estimated
}
