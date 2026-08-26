package contextmanager

import (
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
)

func normalizeHistory(items []llm.ResponseItem, model llm.ModelInfo, estimator Estimator) []llm.ResponseItem {
	result := make([]llm.ResponseItem, 0, len(items))
	pending := make(map[string]string)
	order := make([]string, 0)
	flushMissing := func() {
		for _, callID := range order {
			if _, ok := pending[callID]; ok {
				result = append(result, llm.ToolResultMessage(callID, "Tool execution did not complete before the previous turn ended."))
			}
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
		imageTokens, known := toolContentPartTokenEstimate(content, part)
		if !known || imageTokens > remaining {
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
