package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type CompactRequest struct {
	History      agentcontext.RolloutMessageProjection
	ModelSession *ModelClientSession
	Events       protocol.EventSink
}

func (runtime *Services) Compact(ctx context.Context, request CompactRequest) ([]rollout.RolloutItem, error) {
	if runtime == nil || runtime.client == nil {
		return nil, errors.New("compactor runtime is unavailable")
	}
	if request.ModelSession == nil || request.Events == nil {
		return nil, errors.New("compactor model session is incomplete")
	}
	projection, err := projectCompactionSource(request.History)
	if err != nil {
		return nil, err
	}
	modelMessages, err := runtime.ModelMessages(runtime.ModelInfo())
	if err != nil {
		return nil, err
	}
	compactionInstructions, summaryPrefix, err := internalprompt.CompactionMessages(modelMessages)
	if err != nil {
		return nil, err
	}
	input := agentcontext.NormalizeResponseItems(projection.Covered, runtime.ModelInfo(), nil)
	input = append(input, llm.UserMessage("Create the handoff summary now. Return only the summary and do not call tools."))
	response, err := request.ModelSession.Complete(ctx, CompleteRequest{
		Request: llm.Request{
			Model: runtime.modelInfo.Name, Prompt: llm.Prompt{BaseInstructions: compactionInstructions, Input: input},
		},
		Events: request.Events,
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
	replacement = append(replacement, rollout.ReplacementMessage{Role: string(llm.RoleAssistant), Content: summaryPrefix + "\n\n" + summary})
	encodedSource, err := json.Marshal(projection.Covered)
	if err != nil {
		return nil, fmt.Errorf("encode compaction source: %w", err)
	}
	digest := sha256.Sum256(encodedSource)
	compactionItem := rollout.CompactedItem{
		Summary: summary, ReplacementHistory: replacement,
		CoveredThroughSequence: projection.SourceSequences[len(projection.Covered)-1],
		SourceHash:             hex.EncodeToString(digest[:]), Provider: runtime.providerName, Model: runtime.modelInfo.Name,
	}
	usageItem, err := UsageItem(response.Usage)
	if err != nil {
		return nil, err
	}
	return []rollout.RolloutItem{compactionItem, usageItem}, nil
}

type compactionProjection struct {
	Messages        []llm.ResponseItem
	SourceSequences []int64
	Covered         []llm.ResponseItem
}

func projectCompactionSource(history agentcontext.RolloutMessageProjection) (compactionProjection, error) {
	projection := history.Clone()
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
	return compactionProjection{Messages: projection.Messages, SourceSequences: projection.SourceSequences, Covered: append([]llm.ResponseItem(nil), projection.Messages[:coveredEnd]...)}, nil
}

func firstUser(messages []llm.ResponseItem) *llm.ResponseItem {
	for index := range messages {
		if messages[index].Role == llm.RoleUser {
			message := messages[index]
			return &message
		}
	}
	return nil
}
