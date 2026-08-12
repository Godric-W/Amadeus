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
)

type RolloutMessageProjection struct {
	Messages        []llm.Message
	SourceSequences []int64
}

type rolloutResponseItem struct {
	Type      string          `json:"type"`
	Role      llm.Role        `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Parts     []struct {
		Kind      string `json:"kind"`
		Text      string `json:"text,omitempty"`
		MediaType string `json:"media_type,omitempty"`
		Data      string `json:"data,omitempty"`
	} `json:"parts,omitempty"`
}

type rolloutCompaction struct {
	ReplacementHistory []struct {
		Role    llm.Role `json:"role"`
		Content string   `json:"content"`
	} `json:"replacement_history"`
	CoveredThroughSequence int64  `json:"covered_through_sequence"`
	SourceHash             string `json:"source_hash"`
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
			var payload rolloutCompaction
			if err := json.Unmarshal(line.Item.Payload, &payload); err != nil {
				return RolloutMessageProjection{}, fmt.Errorf("decode compaction at sequence %d: %w", line.Sequence, err)
			}
			if err := projection.applyCompaction(payload); err != nil {
				return RolloutMessageProjection{}, fmt.Errorf("apply compaction at sequence %d: %w", line.Sequence, err)
			}
		}
	}
	return projection, nil
}

func (projection *RolloutMessageProjection) appendResponse(line rollout.Line) error {
	var item rolloutResponseItem
	if err := json.Unmarshal(line.Item.Payload, &item); err != nil {
		return fmt.Errorf("decode response_item at sequence %d: %w", line.Sequence, err)
	}
	switch item.Type {
	case "user_message":
		projection.append(llm.UserMessage(item.Content), line.Sequence)
	case "assistant_message":
		projection.append(llm.AssistantMessage(item.Content), line.Sequence)
	case "tool_call":
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
		projection.append(llm.AssistantToolCallMessage(item.Content, llm.ToolCall{ID: callID, Name: name, Arguments: arguments}), line.Sequence)
	case "tool_result":
		if strings.TrimSpace(item.CallID) == "" {
			return nil
		}
		parts := make([]llm.ContentPart, 0, len(item.Parts))
		for _, part := range item.Parts {
			parts = append(parts, llm.ContentPart{Kind: llm.ContentKind(part.Kind), Text: part.Text, MediaType: part.MediaType, Data: part.Data})
		}
		projection.append(llm.ToolResultMessageWithParts(item.CallID, item.Content, parts...), line.Sequence)
	}
	return nil
}

func (projection *RolloutMessageProjection) append(message llm.Message, sequence uint64) {
	projection.Messages = append(projection.Messages, message)
	projection.SourceSequences = append(projection.SourceSequences, int64(sequence))
}

func (projection *RolloutMessageProjection) applyCompaction(payload rolloutCompaction) error {
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
		replacements = append(replacements, llm.Message{Role: replacement.Role, Content: replacement.Content})
		sequences = append(sequences, payload.CoveredThroughSequence)
	}
	projection.Messages = append(replacements, projection.Messages[covered:]...)
	projection.SourceSequences = append(sequences, projection.SourceSequences[covered:]...)
	return nil
}
