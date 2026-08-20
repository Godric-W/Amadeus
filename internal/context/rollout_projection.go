package agentcontext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type RolloutMessageProjection struct {
	Messages        []llm.ResponseItem
	SourceSequences []int64
}

func ProjectRolloutMessages(lines []rollout.Line) (RolloutMessageProjection, error) {
	projection := RolloutMessageProjection{}
	for _, line := range lines {
		if err := projection.record(line.Sequence, line.Item); err != nil {
			return RolloutMessageProjection{}, err
		}
	}
	return projection, nil
}

func (projection *RolloutMessageProjection) record(sequence uint64, item rollout.RolloutItem) error {
	switch item := item.(type) {
	case rollout.ResponseItem:
		if err := projection.appendResponse(sequence, item); err != nil {
			return fmt.Errorf("project response item at sequence %d: %w", sequence, err)
		}
	case rollout.EventMsgItem:
		switch message := item.Msg.(type) {
		case protocol.TurnAbortedEvent:
			projection.append(llm.DeveloperMessage("Previous turn was interrupted: "+message.Reason+". Re-plan from the current workspace state."), sequence)
		case protocol.TurnCompleteEvent:
			if message.Status == protocol.TurnStatusFailed {
				projection.append(llm.DeveloperMessage("Previous turn failed: "+message.Error+". Re-plan from the current workspace state."), sequence)
			}
		}
	case rollout.CompactedItem:
		if err := projection.applyCompaction(item); err != nil {
			return fmt.Errorf("apply compaction at sequence %d: %w", sequence, err)
		}
	}
	return nil
}

func (projection RolloutMessageProjection) Clone() RolloutMessageProjection {
	return RolloutMessageProjection{
		Messages:        cloneResponseItems(projection.Messages),
		SourceSequences: append([]int64(nil), projection.SourceSequences...),
	}
}

func (projection *RolloutMessageProjection) appendResponse(sequence uint64, item rollout.ResponseItem) error {
	switch item.Type {
	case rollout.ResponseUserMessage:
		projection.append(llm.UserMessage(item.Content), sequence)
	case rollout.ResponseAssistantMessage:
		projection.append(llm.ResponseItem{Role: llm.RoleAssistant, Content: item.Content, Reasoning: item.Reasoning}, sequence)
	case rollout.ResponseToolCall:
		callID := strings.TrimSpace(item.CallID)
		if callID == "" {
			callID = fmt.Sprintf("incomplete-call-%d", sequence)
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = "unknown_tool"
		}
		arguments := append(json.RawMessage(nil), item.Arguments...)
		if len(arguments) == 0 {
			arguments = json.RawMessage(`{}`)
		}
		projection.appendAssistantToolCall(llm.ResponseItem{
			Role: llm.RoleAssistant, Content: item.Content, Reasoning: item.Reasoning,
			ToolCalls: []llm.ToolCall{{ID: callID, Name: name, Arguments: arguments}},
		}, sequence)
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
		projection.append(message, sequence)
	}
	return nil
}

func (projection *RolloutMessageProjection) appendAssistantToolCall(message llm.ResponseItem, sequence uint64) {
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

func (projection *RolloutMessageProjection) append(message llm.ResponseItem, sequence uint64) {
	projection.Messages = append(projection.Messages, message)
	projection.SourceSequences = append(projection.SourceSequences, int64(sequence))
}

func (projection *RolloutMessageProjection) applyCompaction(payload rollout.CompactedItem) error {
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
	replacements := make([]llm.ResponseItem, 0, len(payload.ReplacementHistory))
	sequences := make([]int64, 0, len(payload.ReplacementHistory))
	for _, replacement := range payload.ReplacementHistory {
		replacements = append(replacements, llm.ResponseItem{Role: llm.Role(replacement.Role), Content: replacement.Content})
		sequences = append(sequences, payload.CoveredThroughSequence)
	}
	projection.Messages = append(replacements, projection.Messages[covered:]...)
	projection.SourceSequences = append(sequences, projection.SourceSequences[covered:]...)
	return nil
}
