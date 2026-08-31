package contextmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/Godric-W/Amadeus/internal/llm"
)

func (manager *Manager) Snapshot(model llm.ModelInfo, prompt llm.Prompt) PromptSnapshot {
	if manager == nil {
		return PromptSnapshot{}
	}
	manager.mu.RLock()
	items := cloneResponseItems(manager.items)
	worldStateRevision := manager.worldState.Revision()
	version := manager.lastSequence
	estimator := manager.estimator
	cacheKey := derivedPromptKey(model, prompt, worldStateRevision)
	if cached := manager.snapshotCache; cached != nil && cached.historyVersion == version && cached.worldStateRevision == worldStateRevision && cached.inputKey == cacheKey {
		snapshot := clonePromptSnapshot(cached.snapshot)
		manager.mu.RUnlock()
		return snapshot
	}
	manager.mu.RUnlock()
	model = model.Normalized()
	result := normalizeHistory(items, model, estimator)
	estimatedInputTokens := estimateResponseItems(result, estimator) + estimatePromptOverhead(prompt, estimator)
	revisionInput := struct {
		Items              []llm.ResponseItem
		Prompt             llm.Prompt
		WorldStateRevision string
	}{Items: result, Prompt: prompt, WorldStateRevision: worldStateRevision}
	encoded, _ := json.Marshal(revisionInput)
	promptHash := sha256.Sum256(encoded)
	snapshot := PromptSnapshot{
		Items: result, EstimatedInputTokens: estimatedInputTokens, HistoryVersion: version,
		WorldStateRevision: worldStateRevision, Revision: hex.EncodeToString(promptHash[:]),
	}
	manager.mu.Lock()
	if manager.lastSequence == version && manager.worldState.Revision() == worldStateRevision {
		manager.snapshotCache = &promptSnapshotCache{historyVersion: version, worldStateRevision: worldStateRevision, inputKey: cacheKey, snapshot: clonePromptSnapshot(snapshot)}
	}
	manager.mu.Unlock()
	return snapshot
}

func estimatePromptOverhead(prompt llm.Prompt, estimator Estimator) int64 {
	estimated := estimateResponseItems(prompt.Input, estimator)
	estimated += estimator.EstimateText(prompt.BaseInstructions.Text)
	estimated += estimateToolSpecs(prompt.Tools, estimator)
	if len(prompt.OutputSchema) > 0 {
		estimated += estimator.EstimateText(string(prompt.OutputSchema))
	}
	if prompt.OutputSchemaStrict {
		estimated++
	}
	return estimated
}
