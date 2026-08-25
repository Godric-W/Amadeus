package agentcontext

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
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

type recordState struct {
	projection      RolloutMessageProjection
	updates         map[UpdateKey]contextUpdateState
	lastSequence    uint64
	tokenInfo       *protocol.TokenUsageInfo
	activeTokens    int64
	activeEstimated bool
	tokenSequence   uint64
}

func newRecordState() recordState {
	return recordState{updates: make(map[UpdateKey]contextUpdateState)}
}

func (manager *Manager) recordState() recordState {
	updates := make(map[UpdateKey]contextUpdateState, len(manager.updates))
	for key, value := range manager.updates {
		updates[key] = value
	}
	return recordState{
		projection: RolloutMessageProjection{Messages: cloneResponseItems(manager.items), SourceSequences: append([]int64(nil), manager.sourceSequences...), Origins: append([]MessageOrigin(nil), manager.origins...)},
		updates:    updates, lastSequence: manager.lastSequence,
		tokenInfo: cloneTokenUsageInfo(manager.tokenInfo), activeTokens: manager.activeTokens,
		activeEstimated: manager.activeEstimated, tokenSequence: manager.tokenSequence,
	}
}

func (state *recordState) record(sequence uint64, item rollout.RolloutItem) error {
	if sequence != state.lastSequence+1 {
		return fmt.Errorf("context rollout sequence is %d, expected %d", sequence, state.lastSequence+1)
	}
	if item == nil {
		return errors.New("context rollout item is nil")
	}
	if err := item.Validate(); err != nil {
		return err
	}
	if err := state.projection.record(sequence, item); err != nil {
		return err
	}
	if event, ok := item.(rollout.EventMsgItem); ok {
		switch update := event.Msg.(type) {
		case protocol.ContextUpdateEvent:
			key := UpdateKey(strings.TrimSpace(update.Key))
			if validUpdateKey(key) {
				content := strings.TrimSpace(update.Content)
				if content == "" {
					delete(state.updates, key)
				} else {
					state.updates[key] = contextUpdateState{Content: content, Revision: strings.TrimSpace(update.Revision), Sequence: sequence}
				}
			}
		case protocol.TokenCountEvent:
			state.tokenInfo = cloneTokenUsageInfo(update.Info)
			state.activeTokens = update.ActiveContextTokens
			state.activeEstimated = update.ActiveContextEstimated
			state.tokenSequence = update.ObservedThroughSequence
		}
	}
	state.lastSequence = sequence
	return nil
}

func (manager *Manager) applyRecordState(state recordState) {
	manager.items = state.projection.Messages
	manager.sourceSequences = state.projection.SourceSequences
	manager.origins = state.projection.Origins
	manager.updates = state.updates
	manager.lastSequence = state.lastSequence
	manager.tokenInfo = cloneTokenUsageInfo(state.tokenInfo)
	manager.activeTokens = state.activeTokens
	manager.activeEstimated = state.activeEstimated
	manager.tokenSequence = state.tokenSequence
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

func normalizeHistory(items []llm.ResponseItem, model llm.ModelInfo, estimator Estimator) []llm.ResponseItem {
	result := make([]llm.ResponseItem, 0, len(items))
	pending := make(map[string]string)
	order := make([]string, 0)
	flushMissing := func() {
		for _, callID := range order {
			if _, ok := pending[callID]; !ok {
				continue
			}
			result = append(result, llm.ToolResultMessage(callID, "Tool execution did not complete before the previous turn ended."))
		}
		clear(pending)
		order = order[:0]
	}
	for _, item := range items {
		item = filterResponseItemModalities(item, model)
		if item.Role != llm.RoleTool && len(pending) > 0 {
			flushMissing()
		}
		if len(item.ToolCalls) > 0 {
			if item.Role != llm.RoleAssistant {
				continue
			}
			cloned := cloneResponseItems([]llm.ResponseItem{item})[0]
			validCalls := cloned.ToolCalls[:0]
			for _, call := range cloned.ToolCalls {
				callID := strings.TrimSpace(call.ID)
				if callID == "" {
					continue
				}
				if _, exists := pending[callID]; exists {
					continue
				}
				pending[callID] = strings.TrimSpace(call.Name)
				order = append(order, callID)
				validCalls = append(validCalls, call)
			}
			cloned.ToolCalls = validCalls
			if len(validCalls) > 0 {
				result = append(result, cloned)
			}
			continue
		}
		if item.Role == llm.RoleTool {
			callID := strings.TrimSpace(item.ToolCallID)
			name, exists := pending[callID]
			if !exists {
				continue
			}
			projected := cloneResponseItems([]llm.ResponseItem{item})[0]
			projected.Content = projectToolOutput(name, projected.Content, model.ToolOutputTokenLimit, estimator)
			projected.Content, projected.Parts = projectToolContentParts(name, projected.Content, projected.Parts, model.ToolOutputTokenLimit, estimator)
			result = append(result, projected)
			delete(pending, callID)
			continue
		}
		result = append(result, cloneResponseItems([]llm.ResponseItem{item})[0])
	}
	flushMissing()
	return result
}

func projectToolContentParts(toolName, content string, parts []llm.ContentPart, maximumTokens int64, estimator Estimator) (string, []llm.ContentPart) {
	if len(parts) == 0 || maximumTokens <= 0 {
		return content, parts
	}
	remaining := maximumTokens - estimator.EstimateText(content)
	projected := make([]llm.ContentPart, 0, len(parts))
	omittedImage := false
	for _, part := range parts {
		if part.Kind != llm.ContentImage {
			projected = append(projected, part)
			continue
		}
		imageTokens, imageTokensKnown := toolContentPartTokenEstimate(content, part)
		if !imageTokensKnown || imageTokens > remaining {
			omittedImage = true
			continue
		}
		remaining -= imageTokens
		projected = append(projected, part)
	}
	if omittedImage {
		content = markToolResultImageOmitted(content, "image_budget", "[Image content omitted because it exceeds the current tool output image budget.]")
	}
	textParts := 0
	for _, part := range projected {
		if part.Kind == llm.ContentText && part.Text != "" {
			textParts++
		}
	}
	if textParts == 0 {
		return content, projected
	}
	perPart := remaining / int64(textParts)
	for index := range projected {
		if projected[index].Kind == llm.ContentText && projected[index].Text != "" {
			projected[index].Text = truncateToolText(toolName, projected[index].Text, perPart, estimator)
		}
	}
	return content, projected
}

func filterResponseItemModalities(item llm.ResponseItem, model llm.ModelInfo) llm.ResponseItem {
	if model.SupportsInput(llm.InputModalityImage) {
		return item
	}
	filtered := item.Parts[:0]
	omittedImage := false
	for _, part := range item.Parts {
		if part.Kind == llm.ContentImage {
			omittedImage = true
			continue
		}
		filtered = append(filtered, part)
	}
	item.Parts = filtered
	if !omittedImage {
		return item
	}
	if item.Role == llm.RoleTool {
		item.Content = filterToolResultModalities(item.Content, model)
		return item
	}
	marker := "[Image content omitted because the current model does not support image input.]"
	if strings.TrimSpace(item.Content) == "" {
		item.Content = marker
	} else {
		item.Content = strings.TrimSpace(item.Content) + "\n\n" + marker
	}
	return item
}

func projectToolOutput(toolName, content string, maximumTokens int64, estimator Estimator) string {
	if maximumTokens <= 0 || estimator.EstimateText(content) <= maximumTokens {
		return content
	}
	if projected, ok := truncateToolResultProjection(toolName, content, maximumTokens, estimator); ok {
		return projected
	}
	return truncateToolText(toolName, content, maximumTokens, estimator)
}

func truncateToolText(toolName, content string, maximumTokens int64, estimator Estimator) string {
	label := "tool output"
	switch {
	case strings.Contains(toolName, "search"):
		label = "search results"
	case strings.Contains(toolName, "read") || strings.Contains(toolName, "file"):
		label = "file content"
	case strings.Contains(toolName, "command") || strings.Contains(toolName, "bash") || strings.Contains(toolName, "shell"):
		label = "command output"
	}
	marker := "\n\n[... " + label + " truncated for the model; full output remains in rollout ...]\n\n"
	budget := maximumTokens - estimator.EstimateText(marker)
	if budget <= 0 {
		return strings.TrimSpace(marker)
	}
	runes := []rune(content)
	half := budget / 2
	head := truncateRunesToTokens(runes, half, estimator, false)
	tail := truncateRunesToTokens(runes, budget-estimator.EstimateText(head), estimator, true)
	return head + marker + tail
}

func truncateRunesToTokens(runes []rune, maximum int64, estimator Estimator, fromEnd bool) string {
	if maximum <= 0 || len(runes) == 0 {
		return ""
	}
	low, high := 0, len(runes)
	for low < high {
		middle := (low + high + 1) / 2
		candidate := runes[:middle]
		if fromEnd {
			candidate = runes[len(runes)-middle:]
		}
		if estimator.EstimateText(string(candidate)) <= maximum {
			low = middle
		} else {
			high = middle - 1
		}
	}
	if fromEnd {
		return string(runes[len(runes)-low:])
	}
	return string(runes[:low])
}

func estimateResponseItems(items []llm.ResponseItem, estimator Estimator) int64 {
	var total int64
	for _, item := range items {
		total += estimateResponseItem(item, estimator)
	}
	return total
}

func cloneResponseItems(items []llm.ResponseItem) []llm.ResponseItem {
	cloned := make([]llm.ResponseItem, len(items))
	for index, item := range items {
		cloned[index] = item
		cloned[index].Parts = append([]llm.ContentPart(nil), item.Parts...)
		if item.ToolCalls == nil {
			cloned[index].ToolCalls = nil
			continue
		}
		cloned[index].ToolCalls = make([]llm.ToolCall, len(item.ToolCalls))
		for callIndex, call := range item.ToolCalls {
			cloned[index].ToolCalls[callIndex] = call
			cloned[index].ToolCalls[callIndex].Arguments = append(json.RawMessage(nil), call.Arguments...)
		}
	}
	return cloned
}
