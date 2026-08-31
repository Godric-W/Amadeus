package contextmanager

import (
	"errors"
	"sync"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type PromptSnapshot struct {
	Items                []llm.ResponseItem
	EstimatedInputTokens int64
	HistoryVersion       uint64
	WorldStateRevision   string
	Revision             string
}

type TokenSnapshot struct {
	Info                   *protocol.TokenUsageInfo
	ActiveContextTokens    int64
	ActiveContextEstimated bool
	Sequence               uint64
}

type Manager struct {
	mu               sync.RWMutex
	items            []llm.ResponseItem
	sourceSequences  []int64
	origins          []MessageOrigin
	worldState       WorldStateSnapshot
	worldStateKind   PreviousSectionKind
	referenceContext *rollout.TurnContextItem
	lastSequence     uint64
	tokenInfo        *protocol.TokenUsageInfo
	activeTokens     int64
	activeEstimated  bool
	tokenSequence    uint64
	estimator        Estimator
	snapshotCache    *promptSnapshotCache
	activeTokenCache *activeTokenCache
}

func NewManager(estimator Estimator) *Manager {
	if estimator == nil {
		estimator = ApproxTokenEstimator{}
	}
	return &Manager{worldState: make(WorldStateSnapshot), worldStateKind: PreviousSectionAbsent, estimator: estimator}
}

func NewManagerFromRollout(lines []rollout.Line, estimator Estimator) (*Manager, error) {
	manager := NewManager(estimator)
	if err := manager.Rebuild(lines); err != nil {
		return nil, err
	}
	return manager, nil
}

func (manager *Manager) Rebuild(lines []rollout.Line) error {
	if manager == nil {
		return nil
	}
	state := newRecordState()
	for _, line := range lines {
		if err := state.record(line.Sequence, line.Item); err != nil {
			return err
		}
	}
	manager.mu.Lock()
	manager.applyRecordState(state)
	manager.mu.Unlock()
	return nil
}

func (manager *Manager) Record(firstSequence uint64, items ...rollout.RolloutItem) error {
	if manager == nil || len(items) == 0 {
		return nil
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state := manager.recordState()
	for index, item := range items {
		if err := state.record(firstSequence+uint64(index), item); err != nil {
			return err
		}
	}
	manager.applyRecordState(state)
	return nil
}

func (manager *Manager) ValidateRecord(firstSequence uint64, items ...rollout.RolloutItem) error {
	if manager == nil || len(items) == 0 {
		return nil
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	state := manager.recordState()
	for index, item := range items {
		if err := state.record(firstSequence+uint64(index), item); err != nil {
			return err
		}
	}
	return nil
}

func (manager *Manager) PreviewRecord(firstSequence uint64, model llm.ModelInfo, prompt llm.Prompt, items ...rollout.RolloutItem) (PromptSnapshot, error) {
	if manager == nil {
		return PromptSnapshot{}, errors.New("context manager is nil")
	}
	manager.mu.RLock()
	state := manager.recordState()
	estimator := manager.estimator
	manager.mu.RUnlock()
	for index, item := range items {
		if err := state.record(firstSequence+uint64(index), item); err != nil {
			return PromptSnapshot{}, err
		}
	}
	preview := &Manager{worldState: make(WorldStateSnapshot), estimator: estimator}
	preview.applyRecordState(state)
	return preview.Snapshot(model, prompt), nil
}

func (manager *Manager) NextSequence() uint64 {
	if manager == nil {
		return 1
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.lastSequence + 1
}

func (manager *Manager) RolloutItemCount() int {
	if manager == nil {
		return 0
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return int(manager.lastSequence)
}

func (manager *Manager) Projection() RolloutMessageProjection {
	if manager == nil {
		return RolloutMessageProjection{}
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return RolloutMessageProjection{Messages: cloneResponseItems(manager.items), SourceSequences: append([]int64(nil), manager.sourceSequences...), Origins: append([]MessageOrigin(nil), manager.origins...)}
}

func (manager *Manager) TokenSnapshot() TokenSnapshot {
	if manager == nil {
		return TokenSnapshot{}
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return TokenSnapshot{
		Info: cloneTokenUsageInfo(manager.tokenInfo), ActiveContextTokens: manager.activeTokens,
		ActiveContextEstimated: manager.activeEstimated, Sequence: manager.tokenSequence,
	}
}

func (manager *Manager) ActiveContextTokens(model llm.ModelInfo) (int64, bool) {
	if manager == nil {
		return 0, true
	}
	manager.mu.RLock()
	model = model.Normalized()
	cacheKey := derivedModelKey(model)
	historyVersion := manager.lastSequence
	if cached := manager.activeTokenCache; cached != nil && cached.historyVersion == historyVersion && cached.tokenSequence == manager.tokenSequence && cached.modelKey == cacheKey {
		active, estimated := cached.active, cached.estimated
		manager.mu.RUnlock()
		return active, estimated
	}
	items := cloneResponseItems(manager.items)
	sequences := append([]int64(nil), manager.sourceSequences...)
	tokenInfo := cloneTokenUsageInfo(manager.tokenInfo)
	checkpoint := manager.activeTokens
	estimatedCheckpoint := manager.activeEstimated
	tokenSequence := manager.tokenSequence
	estimator := manager.estimator
	manager.mu.RUnlock()
	if tokenInfo == nil {
		return 0, true
	}
	active := checkpoint
	if active <= 0 {
		active = tokenInfo.LastTokenUsage.TotalTokens
	}
	var suffix []llm.ResponseItem
	for index, sequence := range sequences {
		if uint64(sequence) > tokenSequence {
			suffix = append(suffix, items[index])
		}
	}
	normalizedSuffix := normalizeHistory(suffix, model.Normalized(), estimator)
	suffixTokens := estimateResponseItems(normalizedSuffix, estimator)
	if !estimatedCheckpoint {
		for index := len(normalizedSuffix) - 1; index >= 0; index-- {
			if normalizedSuffix[index].Role == llm.RoleAssistant {
				suffixTokens -= estimateResponseItem(normalizedSuffix[index], estimator)
				break
			}
		}
	}
	active += max(int64(0), suffixTokens)
	estimated := estimatedCheckpoint || len(suffix) > 0
	manager.mu.Lock()
	if manager.lastSequence == historyVersion && manager.tokenSequence == tokenSequence {
		manager.activeTokenCache = &activeTokenCache{historyVersion: historyVersion, tokenSequence: tokenSequence, modelKey: cacheKey, active: active, estimated: estimated}
	}
	manager.mu.Unlock()
	return active, estimated
}

func (manager *Manager) WorldStateBaseline() (WorldStateSnapshot, bool) {
	if manager == nil {
		return nil, false
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.worldState.Clone(), manager.worldStateKind == PreviousSectionKnown
}

func (manager *Manager) WorldStateBaselineKind() PreviousSectionKind {
	if manager == nil {
		return PreviousSectionAbsent
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if manager.worldStateKind == "" {
		return PreviousSectionAbsent
	}
	return manager.worldStateKind
}

func (manager *Manager) ReferenceTurnContext() *rollout.TurnContextItem {
	if manager == nil {
		return nil
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return cloneTurnContextItem(manager.referenceContext)
}

func cloneTokenUsageInfo(info *protocol.TokenUsageInfo) *protocol.TokenUsageInfo {
	if info == nil {
		return nil
	}
	cloned := info.Clone()
	return &cloned
}
