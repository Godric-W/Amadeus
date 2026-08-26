package contextmanager

import (
	"errors"
	"sync"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type UpdateKey string

const (
	UpdateCollaborationMode UpdateKey = "collaboration_mode"
	UpdateAgents            UpdateKey = "agents"
	UpdateEnvironment       UpdateKey = "environment"
	UpdatePermissionMode    UpdateKey = "permission_mode"
	UpdateSkills            UpdateKey = "skills"
	UpdateMCP               UpdateKey = "mcp"
)

var updateOrder = [...]UpdateKey{
	UpdateCollaborationMode,
	UpdateAgents,
	UpdateEnvironment,
	UpdatePermissionMode,
	UpdateSkills,
	UpdateMCP,
}

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

type contextUpdateState struct {
	Content  string
	Revision string
	Sequence uint64
}

// SkillInjection is the explicitly requested portion of a Skill that becomes
// part of the turn's developer context.
type SkillInjection struct {
	Name     string
	Path     string
	Revision string
	Content  string
	Source   string
}

type Manager struct {
	mu              sync.RWMutex
	items           []llm.ResponseItem
	sourceSequences []int64
	origins         []MessageOrigin
	updates         map[UpdateKey]contextUpdateState
	lastSequence    uint64
	tokenInfo       *protocol.TokenUsageInfo
	activeTokens    int64
	activeEstimated bool
	tokenSequence   uint64
	estimator       Estimator
}

func NewManager(estimator Estimator) *Manager {
	if estimator == nil {
		estimator = ApproxTokenEstimator{}
	}
	return &Manager{updates: make(map[UpdateKey]contextUpdateState), estimator: estimator}
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
	preview := &Manager{updates: make(map[UpdateKey]contextUpdateState), estimator: estimator}
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
	items := cloneResponseItems(manager.items)
	sequences := append([]int64(nil), manager.sourceSequences...)
	updates := make([]contextUpdateState, 0, len(manager.updates))
	for _, update := range manager.updates {
		updates = append(updates, update)
	}
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
	for _, update := range updates {
		if update.Sequence > tokenSequence {
			active += estimateResponseItem(llm.DeveloperMessage(update.Content), estimator)
			estimated = true
		}
	}
	return active, estimated
}

func (manager *Manager) Update(key UpdateKey) string {
	if manager == nil || !validUpdateKey(key) {
		return ""
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.updates[key].Content
}

func (manager *Manager) UpdateRevision(key UpdateKey) string {
	if manager == nil || !validUpdateKey(key) {
		return ""
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.updates[key].Revision
}

func cloneTokenUsageInfo(info *protocol.TokenUsageInfo) *protocol.TokenUsageInfo {
	if info == nil {
		return nil
	}
	cloned := info.Clone()
	return &cloned
}

func validUpdateKey(key UpdateKey) bool {
	for _, candidate := range updateOrder {
		if key == candidate {
			return true
		}
	}
	return false
}
