package agentcontext

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type BaseEnvelope = Envelope

type ContextProfile struct {
	ContextWindow                 int64
	EffectiveContextWindowPercent int64
	AutoCompactTokenLimit         int64
}

func (profile ContextProfile) Validate() error {
	if profile.ContextWindow <= 0 {
		return errors.New("context window must be greater than zero")
	}
	if profile.EffectiveContextWindowPercent < 50 || profile.EffectiveContextWindowPercent > 100 {
		return errors.New("effective context window percent must be between 50 and 100")
	}
	effective := profile.EffectiveInputLimit()
	if profile.AutoCompactTokenLimit <= 0 || profile.AutoCompactTokenLimit > effective {
		return errors.New("auto compact token limit must be greater than zero and at most the effective input limit")
	}
	return nil
}

func DefaultContextProfile(contextWindow int64) ContextProfile {
	return ContextProfile{
		ContextWindow:                 contextWindow,
		EffectiveContextWindowPercent: 95,
		AutoCompactTokenLimit:         contextWindow * 90 / 100,
	}
}

func (profile ContextProfile) EffectiveInputLimit() int64 {
	if profile.ContextWindow <= 0 || profile.EffectiveContextWindowPercent <= 0 {
		return 0
	}
	return profile.ContextWindow * profile.EffectiveContextWindowPercent / 100
}

type WindowRequest struct {
	Base          BaseEnvelope
	Runtime       []llm.Message
	Additional    []llm.Message
	Profile       ContextProfile
	PreviousUsage *llm.Usage
	LastSentCount int
}

type ContextUsage struct {
	EstimatedInputTokens int64 `json:"estimated_input_tokens"`
	ToolTokens           int64 `json:"tool_tokens"`
	EffectiveInputLimit  int64 `json:"effective_input_limit"`
	CompressionTarget    int64 `json:"compression_target"`
	ProviderInputTokens  int64 `json:"provider_input_tokens,omitempty"`
}

type CompactionReport struct {
	DroppedMessagePairs  int `json:"dropped_message_pairs,omitempty"`
	ProjectedToolResults int `json:"projected_tool_results,omitempty"`
}

type RequestView struct {
	Messages           []llm.Message
	Tools              []tool.Spec
	Usage              ContextUsage
	Compaction         *CompactionReport
	Revisions          ContextRevisions
	MCPBindingRevision string
	SHA256             string
}

type ContextWindowManager interface {
	Prepare(context.Context, WindowRequest) (RequestView, error)
}

type WindowManager struct {
	estimator Estimator
}

func NewContextWindowManager(estimator Estimator) *WindowManager {
	if estimator == nil {
		estimator = ConservativeEstimator{}
	}
	return &WindowManager{estimator: estimator}
}

func (manager *WindowManager) Prepare(ctx context.Context, request WindowRequest) (RequestView, error) {
	if manager == nil || manager.estimator == nil {
		return RequestView{}, errors.New("context window manager is nil")
	}
	if ctx == nil {
		return RequestView{}, errors.New("context window request context is nil")
	}
	if err := ctx.Err(); err != nil {
		return RequestView{}, err
	}
	if err := request.Profile.Validate(); err != nil {
		return RequestView{}, err
	}
	base := cloneMessages(request.Base.Messages)
	runtime := cloneMessages(request.Runtime)
	additional := cloneMessages(request.Additional)
	tools := cloneSpecs(request.Base.AvailableTools)
	effective := request.Profile.EffectiveInputLimit()
	target := request.Profile.AutoCompactTokenLimit
	toolTokens := estimateTools(tools, manager.estimator)
	report := &CompactionReport{}

	messages := joinWindowMessages(base, additional, runtime)
	estimated := estimateMessages(messages, manager.estimator) + toolTokens
	if estimated > target {
		report.ProjectedToolResults += projectToolResults(base, 2048, manager.estimator)
		report.ProjectedToolResults += projectToolResults(runtime, 2048, manager.estimator)
		messages = joinWindowMessages(base, additional, runtime)
		estimated = estimateMessages(messages, manager.estimator) + toolTokens
	}
	if estimated > target {
		var dropped int
		base, dropped = dropOldestCompletedPairs(base, target-toolTokens-estimateMessages(additional, manager.estimator)-estimateMessages(runtime, manager.estimator), manager.estimator)
		report.DroppedMessagePairs = dropped
		messages = joinWindowMessages(base, additional, runtime)
		estimated = estimateMessages(messages, manager.estimator) + toolTokens
	}
	if estimated > effective {
		return RequestView{}, fmt.Errorf("context request exceeds effective input limit after safe projection: estimated %d, limit %d", estimated, effective)
	}
	usage := ContextUsage{EstimatedInputTokens: estimated, ToolTokens: toolTokens, EffectiveInputLimit: effective, CompressionTarget: target}
	if request.PreviousUsage != nil {
		usage.ProviderInputTokens = request.PreviousUsage.InputTokens
	}
	view := RequestView{Messages: messages, Tools: tools, Usage: usage, Revisions: request.Base.Revisions}
	if report.DroppedMessagePairs > 0 || report.ProjectedToolResults > 0 {
		view.Compaction = report
	}
	encoded, err := json.Marshal(struct {
		Messages  []llm.Message
		Tools     []tool.Spec
		Usage     ContextUsage
		Revisions ContextRevisions
	}{Messages: messages, Tools: tools, Usage: usage, Revisions: request.Base.Revisions})
	if err != nil {
		return RequestView{}, fmt.Errorf("hash context request view: %w", err)
	}
	digest := sha256.Sum256(encoded)
	view.SHA256 = hex.EncodeToString(digest[:])
	return view, nil
}

func projectToolResults(messages []llm.Message, maximumTokens int64, estimator Estimator) int {
	projectedCount := 0
	for index := range messages {
		if messages[index].Role != llm.RoleTool {
			continue
		}
		projected, changed := projectToolResult(messages[index].Content, maximumTokens, estimator)
		if changed {
			messages[index].Content = projected
			projectedCount++
		}
	}
	return projectedCount
}

func joinWindowMessages(base, additional, runtime []llm.Message) []llm.Message {
	result := make([]llm.Message, 0, len(base)+len(additional)+len(runtime))
	result = append(result, base...)
	result = append(result, additional...)
	result = append(result, runtime...)
	return result
}

func dropOldestCompletedPairs(messages []llm.Message, budget int64, estimator Estimator) ([]llm.Message, int) {
	if budget <= 0 {
		return messages, 0
	}
	currentUser := -1
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == llm.RoleUser {
			currentUser = index
			break
		}
	}
	removable := make([][2]int, 0)
	for index := 0; index+1 < currentUser; index++ {
		if messages[index].Role == llm.RoleUser && messages[index+1].Role == llm.RoleAssistant && len(messages[index+1].ToolCalls) == 0 {
			removable = append(removable, [2]int{index, index + 1})
			index++
		}
	}
	drop := make(map[int]struct{})
	dropped := 0
	for _, pair := range removable {
		if estimateMessagesWithout(messages, drop, estimator) <= budget {
			break
		}
		drop[pair[0]] = struct{}{}
		drop[pair[1]] = struct{}{}
		dropped++
	}
	result := make([]llm.Message, 0, len(messages)-len(drop))
	for index, message := range messages {
		if _, removed := drop[index]; !removed {
			result = append(result, message)
		}
	}
	return result, dropped
}

func estimateMessagesWithout(messages []llm.Message, omitted map[int]struct{}, estimator Estimator) int64 {
	var total int64
	for index, message := range messages {
		if _, skip := omitted[index]; !skip {
			total += estimateMessage(message, estimator)
		}
	}
	return total
}

func projectToolResult(content string, maximumTokens int64, estimator Estimator) (string, bool) {
	if estimator.EstimateText(content) <= maximumTokens {
		return content, false
	}
	var payload map[string]any
	if json.Unmarshal([]byte(content), &payload) == nil {
		if text, ok := payload["text"].(string); ok {
			payload["text"] = headTailProjection(text, maximumTokens/2, estimator)
			payload["context_truncated"] = true
			encoded, err := json.Marshal(payload)
			if err == nil && estimator.EstimateText(string(encoded)) <= maximumTokens {
				return string(encoded), true
			}
		}
	}
	return headTailProjection(content, maximumTokens, estimator), true
}

func headTailProjection(content string, maximumTokens int64, estimator Estimator) string {
	const marker = "\n... [context omitted] ...\n"
	if maximumTokens <= estimator.EstimateText(marker) {
		return truncateToTokens(content, maximumTokens, estimator)
	}
	runes := []rune(strings.TrimSpace(content))
	if len(runes) < 2 {
		return string(runes)
	}
	available := maximumTokens - estimator.EstimateText(marker)
	head := truncateToTokens(string(runes[:len(runes)/2]), available/2, estimator)
	tailSource := runes[len(runes)/2:]
	tail := truncateTailToTokens(string(tailSource), available-estimator.EstimateText(head), estimator)
	return head + marker + tail
}

func truncateTailToTokens(content string, maximumTokens int64, estimator Estimator) string {
	runes := []rune(content)
	low, high := 0, len(runes)
	for low < high {
		middle := (low + high) / 2
		if estimator.EstimateText(string(runes[middle:])) <= maximumTokens {
			high = middle
		} else {
			low = middle + 1
		}
	}
	return string(runes[low:])
}

var _ ContextWindowManager = (*WindowManager)(nil)
