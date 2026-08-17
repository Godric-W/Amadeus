package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func (runtime *Services) Compact(ctx context.Context, lines []rollout.Line) ([]rollout.Item, error) {
	if runtime == nil || runtime.client == nil {
		return nil, errors.New("compactor runtime is unavailable")
	}
	projection, err := projectCompactionSource(lines)
	if err != nil {
		return nil, err
	}
	assets, err := internalprompt.LoadAssets()
	if err != nil {
		return nil, err
	}
	input := agentcontext.NormalizeResponseItems(projection.Covered, runtime.client.Model(), nil)
	input = append(input, llm.UserMessage("Compact the covered canonical history into a continuation summary now. Return only Markdown summary; do not call tools."))
	maxOutputTokens := runtime.provider.MaxOutputTokens
	if maxOutputTokens <= 0 || maxOutputTokens > 4096 {
		maxOutputTokens = 4096
	}
	response, err := runtime.client.Complete(ctx, llm.Request{
		Model: runtime.provider.Model, Prompt: llm.Prompt{BaseInstructions: assets.Compaction, Input: input},
		Temperature: 0, MaxOutputTokens: maxOutputTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("generate conversation summary: %w", err)
	}
	summary := strings.TrimSpace(response.Message.Content)
	if summary == "" || len(response.Message.ToolCalls) > 0 {
		return nil, errors.New("compaction model returned no usable summary")
	}
	replacement := make([]rollout.ReplacementMessage, 0, 2)
	if initial := firstUser(projection.Covered); initial != nil {
		replacement = append(replacement, rollout.ReplacementMessage{Role: string(initial.Role), Content: initial.Content})
	}
	replacement = append(replacement, rollout.ReplacementMessage{Role: string(llm.RoleAssistant), Content: "## Compaction Checkpoint\n\n" + summary})
	encodedSource, err := json.Marshal(projection.Covered)
	if err != nil {
		return nil, fmt.Errorf("encode compaction source: %w", err)
	}
	digest := sha256.Sum256(encodedSource)
	compactionItem, err := rollout.NewItem(rollout.KindCompaction, rollout.Compaction{
		Summary: summary, ReplacementHistory: replacement,
		CoveredThroughSequence: projection.SourceSequences[len(projection.Covered)-1],
		SourceHash:             hex.EncodeToString(digest[:]), Provider: runtime.providerName, Model: runtime.provider.Model,
	})
	if err != nil {
		return nil, err
	}
	usageItem, err := UsageItem(response.Usage)
	if err != nil {
		return nil, err
	}
	return []rollout.Item{compactionItem, usageItem}, nil
}

type compactionProjection struct {
	Messages        []llm.Message
	SourceSequences []int64
	Covered         []llm.Message
}

func projectCompactionSource(lines []rollout.Line) (compactionProjection, error) {
	projection, err := agentcontext.ProjectRolloutMessages(lines)
	if err != nil {
		return compactionProjection{}, err
	}
	lastUser := -1
	for index, message := range projection.Messages {
		if message.Role == llm.RoleUser {
			lastUser = index
		}
	}
	if lastUser < 0 {
		return compactionProjection{}, errors.New("conversation has no user history to compact")
	}
	coveredEnd := lastUser
	if lastUser == 0 && len(projection.Messages) > 1 {
		coveredEnd = len(projection.Messages)
	}
	for coveredEnd > 0 && projection.Messages[coveredEnd-1].Role == llm.RoleTool {
		coveredEnd--
	}
	if coveredEnd == 0 {
		return compactionProjection{}, errors.New("conversation has no safely compactable history")
	}
	return compactionProjection{Messages: projection.Messages, SourceSequences: projection.SourceSequences, Covered: append([]llm.Message(nil), projection.Messages[:coveredEnd]...)}, nil
}

func firstUser(messages []llm.Message) *llm.Message {
	for index := range messages {
		if messages[index].Role == llm.RoleUser {
			message := messages[index]
			return &message
		}
	}
	return nil
}
