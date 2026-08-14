package agentcontext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type RolloutMessageProjection struct {
	Messages        []llm.Message
	SourceSequences []int64
}

func ProjectRolloutMessages(lines []rollout.Line) (RolloutMessageProjection, error) {
	projection := RolloutMessageProjection{}
	for _, line := range lines {
		switch line.Item.Kind {
		case rollout.KindResponseItem:
			if err := projection.appendResponse(line); err != nil {
				return RolloutMessageProjection{}, err
			}
		case rollout.KindPlanUpdate:
			projection.append(llm.DeveloperMessage("Current soft execution plan from canonical history:\n"+string(line.Item.Payload)), line.Sequence)
		case rollout.KindTurnAborted:
			payload, err := rollout.DecodePayload[rollout.TurnAborted](line.Item)
			if err != nil {
				return RolloutMessageProjection{}, err
			}
			projection.append(llm.DeveloperMessage("Previous turn was interrupted: "+payload.Reason+". Re-plan from the current workspace state."), line.Sequence)
		case rollout.KindTurnCompleted:
			payload, err := rollout.DecodePayload[rollout.TurnCompleted](line.Item)
			if err != nil {
				return RolloutMessageProjection{}, err
			}
			if payload.Status == rollout.TurnStatusFailed {
				projection.append(llm.DeveloperMessage("Previous turn failed: "+payload.Error+". Re-plan from the current workspace state."), line.Sequence)
			}
		case rollout.KindContextUpdate:
			var update rollout.ContextUpdate
			if err := json.Unmarshal(line.Item.Payload, &update); err != nil {
				return RolloutMessageProjection{}, fmt.Errorf("decode context update at sequence %d: %w", line.Sequence, err)
			}
			if strings.TrimSpace(update.Key) != "" {
				// Dynamic context updates are replayed by ContextManager.Rebuild;
				// they are not mixed into the response-item history here.
				continue
			}
		case rollout.KindCompaction:
			payload, err := rollout.DecodePayload[rollout.Compaction](line.Item)
			if err != nil {
				return RolloutMessageProjection{}, err
			}
			if err := projection.applyCompaction(payload); err != nil {
				return RolloutMessageProjection{}, fmt.Errorf("apply compaction at sequence %d: %w", line.Sequence, err)
			}
		}
	}
	return projection, nil
}

func (projection *RolloutMessageProjection) appendResponse(line rollout.Line) error {
	item, err := rollout.DecodeResponseItem(line.Item)
	if err != nil {
		return fmt.Errorf("decode response_item at sequence %d: %w", line.Sequence, err)
	}
	switch item.Type {
	case rollout.ResponseUserMessage:
		projection.append(llm.UserMessage(item.Content), line.Sequence)
	case rollout.ResponseAssistantMessage:
		projection.append(llm.Message{Role: llm.RoleAssistant, Content: item.Content, Reasoning: item.Reasoning}, line.Sequence)
	case rollout.ResponseToolCall:
		callID := strings.TrimSpace(item.CallID)
		if callID == "" {
			callID = fmt.Sprintf("incomplete-call-%d", line.Sequence)
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = "unknown_tool"
		}
		arguments := append(json.RawMessage(nil), item.Arguments...)
		if len(arguments) == 0 {
			arguments = json.RawMessage(`{}`)
		}
		projection.appendAssistantToolCall(llm.Message{
			Role: llm.RoleAssistant, Content: item.Content, Reasoning: item.Reasoning,
			ToolCalls: []llm.ToolCall{{ID: callID, Name: name, Arguments: arguments}},
		}, line.Sequence)
	case rollout.ResponseToolResult:
		if strings.TrimSpace(item.CallID) == "" {
			return nil
		}
		result := tool.ToolResult{CallID: item.CallID, ToolName: item.Name, Text: item.Content, Parts: append([]tool.ContentPart(nil), item.Parts...)}
		if item.Result != nil {
			result = item.Result.Clone()
		}
		var projectedError *ToolResultError
		if item.Error != nil {
			projectedError = &ToolResultError{Kind: item.Error.Kind, Message: item.Error.Message}
		}
		message, projectErr := ProjectToolResult(ToolResultProjection{
			CallID: item.CallID, Status: item.Status, Result: result, Error: projectedError,
			Partial: item.Partial, Metadata: item.Metadata,
		})
		if projectErr != nil {
			return projectErr
		}
		projection.append(message, line.Sequence)
	}
	return nil
}

func (projection *RolloutMessageProjection) appendAssistantToolCall(message llm.Message, sequence uint64) {
	last := len(projection.Messages) - 1
	if last >= 0 && projection.Messages[last].Role == llm.RoleAssistant &&
		projection.SourceSequences[last]+1 == int64(sequence) {
		if projection.Messages[last].Reasoning == "" {
			projection.Messages[last].Reasoning = message.Reasoning
		}
		if projection.Messages[last].Content == "" {
			projection.Messages[last].Content = message.Content
		}
		projection.Messages[last].ToolCalls = append(projection.Messages[last].ToolCalls, message.ToolCalls...)
		projection.SourceSequences[last] = int64(sequence)
		return
	}
	projection.append(message, sequence)
}

func (projection *RolloutMessageProjection) append(message llm.Message, sequence uint64) {
	projection.Messages = append(projection.Messages, message)
	projection.SourceSequences = append(projection.SourceSequences, int64(sequence))
}

func (projection *RolloutMessageProjection) applyCompaction(payload rollout.Compaction) error {
	covered := 0
	for covered < len(projection.SourceSequences) && projection.SourceSequences[covered] <= payload.CoveredThroughSequence {
		covered++
	}
	if covered == 0 || len(payload.ReplacementHistory) == 0 {
		return errors.New("compaction does not cover projected history")
	}
	encoded, err := json.Marshal(projection.Messages[:covered])
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	if strings.TrimSpace(payload.SourceHash) != "" && hex.EncodeToString(digest[:]) != payload.SourceHash {
		return errors.New("compaction source hash does not match projected history")
	}
	replacements := make([]llm.Message, 0, len(payload.ReplacementHistory))
	sequences := make([]int64, 0, len(payload.ReplacementHistory))
	for _, replacement := range payload.ReplacementHistory {
		replacements = append(replacements, llm.Message{Role: llm.Role(replacement.Role), Content: replacement.Content})
		sequences = append(sequences, payload.CoveredThroughSequence)
	}
	projection.Messages = append(replacements, projection.Messages[covered:]...)
	projection.SourceSequences = append(sequences, projection.SourceSequences[covered:]...)
	return nil
}
