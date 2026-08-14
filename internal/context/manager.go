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
		providerUsage = addUsage(providerUsage, llm.Usage{
			InputTokens: usage.InputTokens, CachedInputTokens: usage.CachedInputTokens,
			OutputTokens: usage.OutputTokens, ReasoningTokens: usage.ReasoningTokens,
			TotalTokens: usage.TotalTokens,
		})
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

func (manager *Manager) Update(key UpdateKey) string {
	if manager == nil || !validUpdateKey(key) {
		return ""
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.updates[key]
}

func addUsage(left, right llm.Usage) llm.Usage {
	return llm.Usage{
		InputTokens:       left.InputTokens + right.InputTokens,
		CachedInputTokens: left.CachedInputTokens + right.CachedInputTokens,
		OutputTokens:      left.OutputTokens + right.OutputTokens,
		ReasoningTokens:   left.ReasoningTokens + right.ReasoningTokens,
		TotalTokens:       left.TotalTokens + right.TotalTokens,
	}
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
			projected.Content = projectToolOutput(name, projected.Content, model.ToolOutputMaxTokens, estimator)
			projected.Parts = projectToolContentParts(name, projected.Content, projected.Parts, model.ToolOutputMaxTokens, estimator)
			result = append(result, projected)
			delete(pending, callID)
			continue
		}
		result = append(result, cloneResponseItems([]llm.ResponseItem{item})[0])
	}
	flushMissing()
	return result
}

func projectToolContentParts(toolName, content string, parts []llm.ContentPart, maximumTokens int64, estimator Estimator) []llm.ContentPart {
	if len(parts) == 0 || maximumTokens <= 0 {
		return parts
	}
	remaining := maximumTokens - estimator.EstimateText(content)
	textParts := 0
	for _, part := range parts {
		if part.Kind == llm.ContentText && part.Text != "" {
			textParts++
		}
	}
	if textParts == 0 {
		return parts
	}
	perPart := remaining / int64(textParts)
	for index := range parts {
		if parts[index].Kind == llm.ContentText && parts[index].Text != "" {
			parts[index].Text = truncateToolText(toolName, parts[index].Text, perPart, estimator)
		}
	}
	return parts
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
