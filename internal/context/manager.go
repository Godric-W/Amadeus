package agentcontext

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type UpdateKey string

const (
	UpdateDeveloperInstructions UpdateKey = "developer_instructions"
	UpdateAgents                UpdateKey = "agents"
	UpdateEnvironment           UpdateKey = "environment"
	UpdatePermissionMode        UpdateKey = "permission_mode"
	UpdateSkills                UpdateKey = "skills"
	UpdateMCP                   UpdateKey = "mcp"
)

var updateOrder = [...]UpdateKey{
	UpdateDeveloperInstructions,
	UpdateAgents,
	UpdateEnvironment,
	UpdatePermissionMode,
	UpdateSkills,
	UpdateMCP,
}

type UsageSnapshot struct {
	ProviderUsage        llm.Usage
	HasProviderUsage     bool
	EstimatedInputTokens int64
}

type PromptSnapshot struct {
	Items          []llm.ResponseItem
	Usage          UsageSnapshot
	HistoryVersion uint64
}

// SkillInjection is the explicitly requested portion of a Skill that becomes
// part of the turn's developer context.
type SkillInjection struct {
	Name        string
	Content     string
	ContentHash string
	Source      string
}

type Manager struct {
	mu             sync.RWMutex
	items          []llm.ResponseItem
	updates        map[UpdateKey]string
	historyVersion uint64
	providerUsage  llm.Usage
	hasUsage       bool
	estimator      Estimator
}

func NewManager(estimator Estimator) *Manager {
	if estimator == nil {
		estimator = ConservativeEstimator{}
	}
	return &Manager{updates: make(map[UpdateKey]string), estimator: estimator}
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
	projection, err := ProjectRolloutMessages(lines)
	if err != nil {
		return err
	}
	updates := make(map[UpdateKey]string)
	var providerUsage llm.Usage
	hasUsage := false
	for _, line := range lines {
		if line.Item.Kind == rollout.KindContextUpdate {
			var update rollout.ContextUpdate
			if err := json.Unmarshal(line.Item.Payload, &update); err != nil {
				return err
			}
			key := UpdateKey(strings.TrimSpace(update.Key))
			if validUpdateKey(key) {
				content := strings.TrimSpace(update.Content)
				if content == "" {
					delete(updates, key)
				} else {
					updates[key] = content
				}
			}
		}
		if line.Item.Kind != rollout.KindTokenUsage {
			continue
		}
		usage, decodeErr := rollout.DecodePayload[rollout.TokenUsage](line.Item)
		if decodeErr != nil {
			return decodeErr
		}
		providerUsage = llm.Usage{
			InputTokens: usage.InputTokens, CachedInputTokens: usage.CachedInputTokens,
			OutputTokens: usage.OutputTokens, ReasoningTokens: usage.ReasoningTokens,
			TotalTokens: usage.TotalTokens,
		}
		hasUsage = true
	}
	manager.mu.Lock()
	manager.items = cloneResponseItems(projection.Messages)
	manager.updates = updates
	manager.providerUsage = providerUsage
	manager.hasUsage = hasUsage
	manager.historyVersion++
	manager.mu.Unlock()
	return nil
}

func (manager *Manager) Record(items ...llm.ResponseItem) {
	if manager == nil || len(items) == 0 {
		return
	}
	manager.mu.Lock()
	manager.items = append(manager.items, cloneResponseItems(items)...)
	manager.historyVersion++
	manager.mu.Unlock()
}

func (manager *Manager) Replace(items ...llm.ResponseItem) {
	if manager == nil {
		return
	}
	manager.mu.Lock()
	manager.items = cloneResponseItems(items)
	manager.historyVersion++
	manager.mu.Unlock()
}

func (manager *Manager) ReplaceUpdate(key UpdateKey, content string) bool {
	if manager == nil || !validUpdateKey(key) {
		return false
	}
	content = strings.TrimSpace(content)
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.updates[key] == content {
		return false
	}
	if content == "" {
		delete(manager.updates, key)
	} else {
		manager.updates[key] = content
	}
	manager.historyVersion++
	return true
}

func (manager *Manager) Update(key UpdateKey) string {
	if manager == nil || !validUpdateKey(key) {
		return ""
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.updates[key]
}

func (manager *Manager) ForPrompt(model llm.ModelInfo) PromptSnapshot {
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
	usage.EstimatedInputTokens = estimateResponseItems(result, estimator)
	return PromptSnapshot{Items: result, Usage: usage, HistoryVersion: version}
}

func (manager *Manager) RawItems() []llm.ResponseItem {
	if manager == nil {
		return nil
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return cloneResponseItems(manager.items)
}

func (manager *Manager) UpdateUsage(usage llm.Usage) {
	if manager == nil {
		return
	}
	manager.mu.Lock()
	manager.providerUsage = usage
	manager.hasUsage = true
	manager.mu.Unlock()
}

func (manager *Manager) Usage() UsageSnapshot {
	if manager == nil {
		return UsageSnapshot{}
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return UsageSnapshot{ProviderUsage: manager.providerUsage, HasProviderUsage: manager.hasUsage}
}

func (manager *Manager) HistoryVersion() uint64 {
	if manager == nil {
		return 0
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.historyVersion
}

func (manager *Manager) EstimatePromptTokens(model llm.ModelInfo, prompt llm.Prompt) int64 {
	if manager == nil {
		return 0
	}
	snapshot := manager.ForPrompt(model)
	additional := estimateResponseItems(prompt.Input, manager.estimator)
	overhead := manager.estimator.EstimateText(prompt.BaseInstructions.Text)
	if encoded, err := json.Marshal(prompt.Tools); err == nil {
		overhead += manager.estimator.EstimateText(string(encoded))
	}
	if len(prompt.OutputSchema) > 0 {
		overhead += manager.estimator.EstimateText(string(prompt.OutputSchema))
	}
	return snapshot.Usage.EstimatedInputTokens + additional + overhead
}

func (manager *Manager) NeedsCompaction(model llm.ModelInfo, prompt llm.Prompt) bool {
	model = model.Normalized()
	if model.AutoCompactTokenLimit <= 0 && model.ContextWindow <= 0 {
		return false
	}
	estimated := manager.EstimatePromptTokens(model, prompt)
	if model.AutoCompactTokenLimit > 0 && estimated >= model.AutoCompactTokenLimit {
		return true
	}
	return model.ContextWindow > 0 && estimated+int64(model.MaxOutputTokens) >= model.ContextWindow
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
			projected.Content = projectToolOutput(name, projected.Content, model.ToolOutputMaxTokens, estimator)
			result = append(result, projected)
			delete(pending, callID)
			continue
		}
		result = append(result, cloneResponseItems([]llm.ResponseItem{item})[0])
	}
	flushMissing()
	return result
}

func projectToolOutput(toolName, content string, maximumTokens int64, estimator Estimator) string {
	if maximumTokens <= 0 || estimator.EstimateText(content) <= maximumTokens {
		return content
	}
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
		encoded, _ := json.Marshal(item)
		total += estimator.EstimateText(string(encoded)) + 4
	}
	return total
}

func cloneResponseItems(items []llm.ResponseItem) []llm.ResponseItem {
	cloned := make([]llm.ResponseItem, len(items))
	for index, item := range items {
		cloned[index] = item
		cloned[index].Parts = append([]llm.ContentPart(nil), item.Parts...)
		cloned[index].ToolCalls = make([]llm.ToolCall, len(item.ToolCalls))
		for callIndex, call := range item.ToolCalls {
			cloned[index].ToolCalls[callIndex] = call
			cloned[index].ToolCalls[callIndex].Arguments = append(json.RawMessage(nil), call.Arguments...)
		}
	}
	return cloned
}
