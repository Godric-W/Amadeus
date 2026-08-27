package contextmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type RolloutMessageProjection struct {
	Messages        []llm.ResponseItem
	SourceSequences []int64
	Origins         []MessageOrigin
}

type MessageOrigin string

const (
	MessageOriginUser       MessageOrigin = "user"
	MessageOriginAssistant  MessageOrigin = "assistant"
	MessageOriginTool       MessageOrigin = "tool"
	MessageOriginRuntime    MessageOrigin = "runtime"
	MessageOriginSubagent   MessageOrigin = "subagent"
	MessageOriginCompaction MessageOrigin = "compaction"
)

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
			projection.append(llm.DeveloperMessage("Previous turn was interrupted: "+message.Reason+". Re-plan from the current workspace state."), sequence, MessageOriginRuntime)
		case protocol.TurnCompleteEvent:
			if message.Status == protocol.TurnStatusFailed {
				projection.append(llm.DeveloperMessage("Previous turn failed: "+message.Error+". Re-plan from the current workspace state."), sequence, MessageOriginRuntime)
			}
		case protocol.SubagentNotificationEvent:
			projection.append(llm.UserMessage(message.Content), sequence, MessageOriginSubagent)
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
		Origins:         append([]MessageOrigin(nil), projection.Origins...),
	}
}

func (projection *RolloutMessageProjection) appendResponse(sequence uint64, item rollout.ResponseItem) error {
	switch item.Type {
	case rollout.ResponseUserMessage:
		projection.append(llm.UserMessage(item.Content), sequence, MessageOriginUser)
	case rollout.ResponseContextMessage:
		role := llm.Role(item.Role)
		if role != llm.RoleDeveloper && role != llm.RoleUser {
			return errors.New("context response role is invalid")
		}
		projection.append(llm.ResponseItem{Role: role, Content: item.Content}, sequence, MessageOriginRuntime)
	case rollout.ResponseAssistantMessage:
		projection.append(llm.ResponseItem{Role: llm.RoleAssistant, Content: item.Content, Reasoning: item.Reasoning}, sequence, MessageOriginAssistant)
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
			if len(item.Parts) > 0 {
				result.Parts = append([]tool.ContentPart(nil), item.Parts...)
			}
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
		projection.append(message, sequence, MessageOriginTool)
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
	projection.append(message, sequence, MessageOriginAssistant)
}

func (projection *RolloutMessageProjection) append(message llm.ResponseItem, sequence uint64, origin MessageOrigin) {
	projection.Messages = append(projection.Messages, message)
	projection.SourceSequences = append(projection.SourceSequences, int64(sequence))
	projection.Origins = append(projection.Origins, origin)
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
	origins := make([]MessageOrigin, 0, len(payload.ReplacementHistory))
	for index, replacement := range payload.ReplacementHistory {
		replacements = append(replacements, cloneResponseItems([]llm.ResponseItem{replacement})[0])
		sequences = append(sequences, payload.CoveredThroughSequence)
		var origin MessageOrigin
		switch payload.ReplacementOrigins[index] {
		case rollout.ReplacementOriginUser:
			origin = MessageOriginUser
		case rollout.ReplacementOriginRuntime:
			origin = MessageOriginRuntime
		case rollout.ReplacementOriginCompaction:
			origin = MessageOriginCompaction
		default:
			return errors.New("compaction replacement origin is invalid")
		}
		origins = append(origins, origin)
	}
	projection.Messages = append(replacements, projection.Messages[covered:]...)
	projection.SourceSequences = append(sequences, projection.SourceSequences[covered:]...)
	projection.Origins = append(origins, projection.Origins[covered:]...)
	return nil
}
