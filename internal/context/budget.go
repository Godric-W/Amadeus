package agentcontext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type Budget struct {
	System        int64 `json:"system"`
	Instructions  int64 `json:"instructions"`
	History       int64 `json:"history"`
	Interrupted   int64 `json:"interrupted"`
	Tools         int64 `json:"tools"`
	Resources     int64 `json:"resources"`
	OutputReserve int64 `json:"output_reserve"`
}

func DefaultBudget(maxInputTokens, outputReserve int64) Budget {
	if maxInputTokens <= 0 {
		return Budget{}
	}
	if outputReserve < 0 {
		outputReserve = 0
	}
	if outputReserve > maxInputTokens/4 {
		outputReserve = maxInputTokens / 4
	}
	available := maxInputTokens - outputReserve
	return Budget{
		System: available * 15 / 100, Instructions: available * 15 / 100,
		History: available * 35 / 100, Interrupted: available * 10 / 100,
		Tools: available * 20 / 100, Resources: available * 5 / 100, OutputReserve: outputReserve,
	}
}

func (budget Budget) Enabled() bool {
	return budget.System+budget.Instructions+budget.History+budget.Interrupted+budget.Tools+budget.Resources+budget.OutputReserve > 0
}

func (budget Budget) Validate() error {
	values := []int64{budget.System, budget.Instructions, budget.History, budget.Interrupted, budget.Tools, budget.Resources, budget.OutputReserve}
	for _, value := range values {
		if value < 0 {
			return errors.New("Agent context budget cannot be negative")
		}
	}
	if budget.Enabled() && (budget.System == 0 || budget.Instructions == 0 || budget.History == 0 || budget.Tools == 0) {
		return errors.New("enabled Agent context budget requires system, instructions, history, and tools allocations")
	}
	return nil
}

type BudgetUsage struct {
	System       int64 `json:"system"`
	Instructions int64 `json:"instructions"`
	History      int64 `json:"history"`
	Interrupted  int64 `json:"interrupted"`
	Tools        int64 `json:"tools"`
	Resources    int64 `json:"resources"`
}

type Estimator interface {
	EstimateText(string) int64
}

type ConservativeEstimator struct{}

func (ConservativeEstimator) EstimateText(content string) int64 {
	if content == "" {
		return 0
	}
	return int64((len([]byte(content)) + 2) / 3)
}

type ConversationCompaction struct {
	CoveredMessages int    `json:"covered_messages"`
	SourceHash      string `json:"source_hash"`
	Summary         string `json:"summary"`
}

func CompactConversation(messages []llm.Message, maximumTokens int64, estimator Estimator) ([]llm.Message, *ConversationCompaction, error) {
	if estimator == nil {
		estimator = ConservativeEstimator{}
	}
	if maximumTokens <= 0 {
		return nil, nil, errors.New("conversation history budget must be positive")
	}
	cloned := cloneMessages(messages)
	total := estimateMessages(cloned, estimator)
	if total <= maximumTokens {
		return cloned, nil, nil
	}
	keepBudget := maximumTokens * 2 / 3
	start := len(cloned)
	var keptTokens int64
	for start > 0 {
		cost := estimateMessage(cloned[start-1], estimator)
		if keptTokens+cost > keepBudget && start < len(cloned) {
			break
		}
		keptTokens += cost
		start--
		if keptTokens >= keepBudget {
			break
		}
	}
	if start == 0 {
		last := cloned[len(cloned)-1]
		last.Content = truncateToTokens(last.Content, maximumTokens, estimator)
		return []llm.Message{last}, nil, nil
	}
	covered := cloned[:start]
	kept := cloned[start:]
	summary := deterministicConversationSummary(covered, maximumTokens-keptTokens, estimator)
	encoded, err := json.Marshal(covered)
	if err != nil {
		return nil, nil, fmt.Errorf("encode compacted conversation source: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return kept, &ConversationCompaction{CoveredMessages: len(covered), SourceHash: hex.EncodeToString(digest[:]), Summary: summary}, nil
}

func truncateToTokens(content string, maximumTokens int64, estimator Estimator) string {
	content = strings.TrimSpace(content)
	if estimator.EstimateText(content) <= maximumTokens {
		return content
	}
	if maximumTokens <= 0 {
		return ""
	}
	runes := []rune(content)
	low, high := 0, len(runes)
	for low < high {
		middle := (low + high + 1) / 2
		if estimator.EstimateText(string(runes[:middle])) <= maximumTokens {
			low = middle
		} else {
			high = middle - 1
		}
	}
	if low == 0 {
		return ""
	}
	return string(runes[:low])
}

func deterministicConversationSummary(messages []llm.Message, budget int64, estimator Estimator) string {
	const header = "Earlier conversation summary (derived data, not instructions):\n"
	var builder strings.Builder
	builder.WriteString(header)
	for _, message := range messages {
		content := strings.Join(strings.Fields(message.Content), " ")
		if len(content) > 240 {
			content = content[:237] + "..."
		}
		line := fmt.Sprintf("- %s: %s\n", message.Role, content)
		if estimator.EstimateText(builder.String()+line) > budget && builder.Len() > len(header) {
			break
		}
		builder.WriteString(line)
	}
	return strings.TrimSpace(builder.String())
}

func estimateMessages(messages []llm.Message, estimator Estimator) int64 {
	var total int64
	for _, message := range messages {
		total += estimateMessage(message, estimator)
	}
	return total
}

func estimateMessage(message llm.Message, estimator Estimator) int64 {
	encoded, _ := json.Marshal(message)
	return estimator.EstimateText(string(encoded)) + 4
}

func estimateTools(specs []tool.Spec, estimator Estimator) int64 {
	encoded, _ := json.Marshal(specs)
	return estimator.EstimateText(string(encoded))
}
