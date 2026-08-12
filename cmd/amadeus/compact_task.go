package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/task"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func (runner *agentController) executeCompactTurn(ctx context.Context, factory *codingTaskFactory, host task.Host, _ *turn.Context) (task.Result, error) {
	projection, err := projectCompactionSource(host.History())
	if err != nil {
		return task.Result{}, err
	}
	if len(projection.Messages) == 0 || len(projection.SourceSequences) != len(projection.Messages) {
		return task.Result{}, errors.New("there is no conversation to compact")
	}
	providerName := factory.configured.DefaultProvider
	provider, ok := factory.configured.Providers[providerName]
	if !ok {
		return task.Result{}, fmt.Errorf("default Provider %q is not configured", providerName)
	}
	createClient := runner.runtime.llmClientFactory
	if createClient == nil {
		createClient = defaultLLMClientFactory
	}
	client, err := createClient(providerName, provider)
	if err != nil {
		return task.Result{}, err
	}
	assets, err := internalprompt.LoadAssets()
	if err != nil {
		return task.Result{}, err
	}
	compactionContext := agentcontext.NewManager(nil)
	compactionContext.Record(projection.Covered...)
	input := compactionContext.ForPrompt(client.Model()).Items
	input = append(input, llm.UserMessage("Compact the covered canonical history into a continuation summary now. Return only Markdown summary; do not call tools."))
	maxOutputTokens := provider.MaxOutputTokens
	if maxOutputTokens <= 0 || maxOutputTokens > 4096 {
		maxOutputTokens = 4096
	}
	response, err := client.Complete(ctx, llm.Request{
		Model:       provider.Model,
		Prompt:      llm.Prompt{BaseInstructions: assets.Compaction, Input: input},
		Temperature: 0, MaxOutputTokens: maxOutputTokens,
	})
	if err != nil {
		return task.Result{}, fmt.Errorf("generate conversation summary: %w", err)
	}
	summary := strings.TrimSpace(response.Message.Content)
	if summary == "" || len(response.Message.ToolCalls) > 0 {
		return task.Result{}, errors.New("compaction model returned no usable summary")
	}
	checkpoint := "## Compaction Checkpoint\n\n" + summary
	replacement := make([]map[string]any, 0, 2)
	if initial := firstUser(projection.Covered); initial != nil {
		replacement = append(replacement, map[string]any{"role": initial.Role, "content": initial.Content})
	}
	replacement = append(replacement, map[string]any{"role": llm.RoleAssistant, "content": checkpoint})
	encodedSource, err := json.Marshal(projection.Covered)
	if err != nil {
		return task.Result{}, fmt.Errorf("encode compaction source: %w", err)
	}
	digest := sha256.Sum256(encodedSource)
	compactionItem, err := rollout.NewRawItem(rollout.KindCompaction, mustMarshalRaw(map[string]any{
		"summary": summary, "replacement_history": replacement,
		"covered_through_sequence": projection.SourceSequences[len(projection.Covered)-1],
		"source_hash":              hex.EncodeToString(digest[:]), "provider": providerName, "model": provider.Model,
	}))
	if err != nil {
		return task.Result{}, err
	}
	usageItem, err := rollout.NewItem(rollout.KindTokenUsage, rollout.TokenUsage{
		InputTokens: response.Usage.InputTokens, CachedInputTokens: response.Usage.CachedInputTokens,
		OutputTokens: response.Usage.OutputTokens, ReasoningTokens: response.Usage.ReasoningTokens,
		TotalTokens: response.Usage.TotalTokens,
	})
	if err != nil {
		return task.Result{}, err
	}
	return task.Result{Items: []rollout.Item{compactionItem, usageItem}}, nil
}

type compactionProjection struct {
	Messages        []llm.Message
	SourceSequences []int64
	Covered         []llm.Message
}

func projectCompactionSource(lines []rollout.Line) (compactionProjection, error) {
	projection, err := projectRolloutMessagesForCompaction(lines)
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

func projectRolloutMessagesForCompaction(lines []rollout.Line) (agentcontext.RolloutMessageProjection, error) {
	return agentcontext.ProjectRolloutMessages(lines)
}
