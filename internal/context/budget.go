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
	CoveredMessages        int           `json:"covered_messages"`
	CoveredThroughSequence int64         `json:"covered_through_sequence,omitempty"`
	SourceHash             string        `json:"source_hash"`
	Summary                string        `json:"summary"`
	ReplacementHistory     []llm.Message `json:"replacement_history"`
}

func CompactConversation(messages []llm.Message, maximumTokens int64, estimator Estimator) ([]llm.Message, *ConversationCompaction, error) {
	return CompactConversationWithSources(messages, nil, maximumTokens, estimator)
}

func CompactConversationWithSources(messages []llm.Message, sourceSequences []int64, maximumTokens int64, estimator Estimator) ([]llm.Message, *ConversationCompaction, error) {
	if estimator == nil {
		estimator = ConservativeEstimator{}
	}
	if maximumTokens <= 0 {
		return nil, nil, errors.New("conversation history budget must be positive")
	}
	cloned := cloneMessages(messages)
	if len(sourceSequences) > 0 {
		if len(sourceSequences) != len(cloned) {
			return nil, nil, errors.New("conversation source sequence count does not match messages")
		}
		for index, sequence := range sourceSequences {
			if sequence < 1 || (index > 0 && sequence < sourceSequences[index-1]) {
				return nil, nil, errors.New("conversation source sequences must be positive and non-decreasing")
			}
		}
	}
	total := estimateMessages(cloned, estimator)
	if total <= maximumTokens {
		return cloned, nil, nil
	}
	groups, err := atomicConversationGroups(cloned, sourceSequences)
	if err != nil {
		return nil, nil, err
	}
	keepBudget := maximumTokens * 2 / 3
	startGroup := len(groups)
	var keptTokens int64
	for startGroup > 0 {
		group := groups[startGroup-1]
		cost := estimateMessages(cloned[group.start:group.end], estimator)
		if keptTokens+cost > keepBudget && startGroup < len(groups) {
			break
		}
		keptTokens += cost
		startGroup--
		if keptTokens >= keepBudget {
			break
		}
	}
	if startGroup == 0 {
		return nil, nil, fmt.Errorf("conversation history exceeds budget but contains no safely compactable atomic group: estimated %d, budget %d", total, maximumTokens)
	}
	start := groups[startGroup].start
	covered := cloned[:start]
	kept := cloned[start:]
	summary := deterministicReplacementHistory(covered, maximumTokens-keptTokens, estimator)
	encoded, err := json.Marshal(covered)
	if err != nil {
		return nil, nil, fmt.Errorf("encode compacted conversation source: %w", err)
	}
	digest := sha256.Sum256(encoded)
	compaction := &ConversationCompaction{
		CoveredMessages:    len(covered),
		SourceHash:         hex.EncodeToString(digest[:]),
		Summary:            summary,
		ReplacementHistory: []llm.Message{llm.AssistantMessage(summary)},
	}
	if len(sourceSequences) > 0 {
		compaction.CoveredThroughSequence = sourceSequences[start-1]
	}
	return kept, compaction, nil
}

type conversationGroup struct {
	start int
	end   int
}

func atomicConversationGroups(messages []llm.Message, sourceSequences []int64) ([]conversationGroup, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	groups := make([]conversationGroup, 0, len(messages))
	start := 0
	for index := 1; index < len(messages); index++ {
		if messages[index].Role == llm.RoleUser {
			groups = append(groups, conversationGroup{start: start, end: index})
			start = index
		}
	}
	groups = append(groups, conversationGroup{start: start, end: len(messages)})

	merged := groups[:0]
	for _, group := range groups {
		if err := validateToolProtocolGroup(messages[group.start:group.end]); err != nil {
			return nil, err
		}
		if len(merged) > 0 && len(sourceSequences) > 0 && sourceSequences[merged[len(merged)-1].end-1] == sourceSequences[group.start] {
			merged[len(merged)-1].end = group.end
			continue
		}
		merged = append(merged, group)
	}
	return merged, nil
}

func validateToolProtocolGroup(messages []llm.Message) error {
	pending := make(map[string]struct{})
	for index, message := range messages {
		if len(pending) > 0 && message.Role != llm.RoleTool {
			return fmt.Errorf("conversation Tool Call group is interrupted before all results at message %d", index)
		}
		if len(message.ToolCalls) > 0 {
			if message.Role != llm.RoleAssistant || len(pending) > 0 {
				return fmt.Errorf("conversation message %d starts an invalid Tool Call group", index)
			}
			for _, call := range message.ToolCalls {
				callID := strings.TrimSpace(call.ID)
				if callID == "" {
					return fmt.Errorf("conversation message %d contains an empty Tool Call ID", index)
				}
				if _, duplicate := pending[callID]; duplicate {
					return fmt.Errorf("conversation message %d contains duplicate Tool Call ID %q", index, callID)
				}
				pending[callID] = struct{}{}
			}
			continue
		}
		if message.Role != llm.RoleTool {
			continue
		}
		callID := strings.TrimSpace(message.ToolCallID)
		if _, exists := pending[callID]; !exists {
			return fmt.Errorf("conversation message %d contains orphan Tool Result %q", index, callID)
		}
		delete(pending, callID)
	}
	if len(pending) > 0 {
		return errors.New("conversation contains an incomplete Tool Call group")
	}
	return nil
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

func deterministicReplacementHistory(messages []llm.Message, budget int64, estimator Estimator) string {
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
