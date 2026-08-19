package agentcontext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
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
	worldStateParts := make([]string, 0, len(manager.updates))
	for key, value := range manager.updates {
		updates[key] = value.Content
		worldStateParts = append(worldStateParts, string(key)+":"+value.Revision)
	}
	version := manager.historyVersion
	usage := UsageSnapshot{ProviderUsage: manager.providerUsage, HasProviderUsage: manager.hasUsage}
	estimator := manager.estimator
	manager.mu.RUnlock()
	sort.Strings(worldStateParts)
	worldStateHash := sha256.Sum256([]byte(strings.Join(worldStateParts, "\n")))
	worldStateRevision := hex.EncodeToString(worldStateHash[:])

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
	revisionInput := struct {
		Items              []llm.ResponseItem
		Prompt             llm.Prompt
		WorldStateRevision string
	}{Items: result, Prompt: prompt, WorldStateRevision: worldStateRevision}
	encoded, _ := json.Marshal(revisionInput)
	promptHash := sha256.Sum256(encoded)
	return PromptSnapshot{
		Items: result, Usage: usage, HistoryVersion: version,
		WorldStateRevision: worldStateRevision, Revision: hex.EncodeToString(promptHash[:]),
	}
}

func (snapshot PromptSnapshot) NeedsCompaction(model llm.ModelInfo) bool {
	model = model.Normalized()
	if model.AutoCompactTokenLimit > 0 && snapshot.Usage.EstimatedInputTokens >= model.AutoCompactTokenLimit {
		return true
	}
	return model.ContextWindow > 0 && snapshot.Usage.EstimatedInputTokens >= model.ContextWindow
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
	if prompt.OutputSchemaStrict {
		estimated++
	}
	return estimated
}
