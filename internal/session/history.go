package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/llm"
)

type HistoryView struct {
	Session  Session
	Items    []RolloutItem
	Revision int64
}

type MessageProjection struct {
	Messages        []llm.Message
	SourceSequences []int64
}

type SessionHistory struct {
	mutex    sync.RWMutex
	session  Session
	items    []RolloutItem
	revision int64
}

func NewSessionHistory(value Session, items []RolloutItem) (*SessionHistory, error) {
	if err := value.Validate(); err != nil {
		return nil, fmt.Errorf("session history: %w", err)
	}
	cloned := cloneItems(items)
	if err := validateHistoryItems(value.ID, cloned); err != nil {
		return nil, err
	}
	return &SessionHistory{session: value, items: cloned, revision: int64(len(cloned))}, nil
}

func (history *SessionHistory) View() HistoryView {
	if history == nil {
		return HistoryView{}
	}
	history.mutex.RLock()
	defer history.mutex.RUnlock()
	return HistoryView{Session: history.session, Items: cloneItems(history.items), Revision: history.revision}
}

func (history *SessionHistory) UpdateSession(value Session) error {
	if history == nil {
		return errors.New("session history is nil")
	}
	if err := value.Validate(); err != nil {
		return fmt.Errorf("update session history: %w", err)
	}
	history.mutex.Lock()
	defer history.mutex.Unlock()
	if history.session.ID != value.ID {
		return errors.New("session history update ID does not match")
	}
	history.session = value
	history.revision++
	return nil
}

func (history *SessionHistory) ProjectMessages() (MessageProjection, error) {
	if history == nil {
		return MessageProjection{}, errors.New("session history is nil")
	}
	history.mutex.RLock()
	items := cloneItems(history.items)
	history.mutex.RUnlock()
	return ProjectMessages(items)
}

func ProjectMessages(items []RolloutItem) (MessageProjection, error) {
	projection := MessageProjection{
		Messages:        make([]llm.Message, 0, len(items)),
		SourceSequences: make([]int64, 0, len(items)),
	}
	for index, item := range items {
		if err := item.Validate(); err != nil {
			return MessageProjection{}, fmt.Errorf("project rollout item %d: %w", index, err)
		}
		switch item.Kind {
		case RolloutUserMessage:
			payload, err := DecodeUserMessage(item)
			if err != nil {
				return MessageProjection{}, fmt.Errorf("project user message: %w", err)
			}
			projection.append(llm.UserMessage(payload.Content), item.Sequence)
		case RolloutAssistantMessage:
			payload, err := DecodeAssistantMessage(item)
			if err != nil {
				return MessageProjection{}, fmt.Errorf("project assistant message: %w", err)
			}
			projection.append(llm.AssistantMessage(payload.Content), item.Sequence)
		case RolloutToolCall:
			payload, err := DecodeToolCall(item)
			if err != nil {
				return MessageProjection{}, fmt.Errorf("project tool call: %w", err)
			}
			calls := make([]llm.ToolCall, 0, len(payload.Calls))
			for _, call := range payload.Calls {
				calls = append(calls, llm.ToolCall{ID: call.ID, Name: call.Name, Arguments: append(json.RawMessage(nil), call.Arguments...)})
			}
			projection.append(llm.AssistantToolCallMessage(payload.Content, calls...), item.Sequence)
		case RolloutToolResult:
			payload, err := DecodeToolResult(item)
			if err != nil {
				return MessageProjection{}, fmt.Errorf("project tool result: %w", err)
			}
			content, parts, err := projectToolResult(payload)
			if err != nil {
				return MessageProjection{}, err
			}
			projection.append(llm.ToolResultMessageWithParts(payload.CallID, content, parts...), item.Sequence)
		case RolloutRunInterrupted, RolloutRunFailed:
			payload, err := DecodeRunMarker(item)
			if err != nil {
				return MessageProjection{}, fmt.Errorf("project Run marker: %w", err)
			}
			label := "interrupted"
			if item.Kind == RolloutRunFailed {
				label = "failed"
			}
			content := fmt.Sprintf("Previous Run %s: %s", label, payload.Reason)
			if payload.Guidance != "" {
				content += ". " + payload.Guidance
			}
			projection.append(llm.DeveloperMessage(content), item.Sequence)
		case RolloutPlanUpdate:
			payload, err := DecodePlanUpdate(item)
			if err != nil {
				return MessageProjection{}, fmt.Errorf("project plan update: %w", err)
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				return MessageProjection{}, fmt.Errorf("project plan update: %w", err)
			}
			projection.append(llm.DeveloperMessage("Current soft execution plan from canonical history:\n"+string(encoded)), item.Sequence)
		case RolloutContextCompaction:
			payload, err := DecodeContextCompaction(item)
			if err != nil {
				continue
			}
			if err := projection.applyCompaction(payload); err != nil {
				continue
			}
		case RolloutContextSnapshot:
			continue
		default:
			return MessageProjection{}, fmt.Errorf("project rollout kind %q is unsupported", item.Kind)
		}
	}
	return projection, nil
}

func (projection *MessageProjection) append(message llm.Message, sequence int64) {
	projection.Messages = append(projection.Messages, message)
	projection.SourceSequences = append(projection.SourceSequences, sequence)
}

func (projection *MessageProjection) applyCompaction(payload ContextCompactionPayload) error {
	covered := 0
	for covered < len(projection.SourceSequences) && projection.SourceSequences[covered] <= payload.CoveredThroughSequence {
		covered++
	}
	if covered == 0 {
		return errors.New("covered sequence does not match projected history")
	}
	encoded, err := json.Marshal(projection.Messages[:covered])
	if err != nil {
		return fmt.Errorf("encode compaction source: %w", err)
	}
	digest := sha256.Sum256(encoded)
	if hex.EncodeToString(digest[:]) != payload.SourceHash {
		return errors.New("source hash does not match projected history")
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

func projectToolResult(payload ToolResultPayload) (string, []llm.ContentPart, error) {
	type modelPayload struct {
		OK       bool              `json:"ok"`
		Text     string            `json:"text,omitempty"`
		Parts    []ToolContentPart `json:"parts,omitempty"`
		Metadata map[string]any    `json:"metadata,omitempty"`
		Partial  bool              `json:"partial,omitempty"`
		Error    string            `json:"error,omitempty"`
	}
	projected := modelPayload{OK: payload.Status == "succeeded", Text: payload.Text, Parts: payload.Parts, Metadata: payload.Metadata, Partial: payload.Partial}
	if payload.Error != nil {
		projected.Error = strings.TrimSpace(payload.Error.Message)
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		return "", nil, fmt.Errorf("project tool result %q: %w", payload.CallID, err)
	}
	parts := make([]llm.ContentPart, 0, len(payload.Parts))
	for _, part := range payload.Parts {
		switch part.Kind {
		case "text":
			parts = append(parts, llm.TextPart(part.Text))
		case "image":
			parts = append(parts, llm.ImagePart(part.MediaType, part.Data))
		default:
			return "", nil, fmt.Errorf("project tool result %q has unsupported content kind %q", payload.CallID, part.Kind)
		}
	}
	return string(encoded), parts, nil
}

func (history *SessionHistory) Append(value Session, items ...RolloutItem) error {
	if history == nil {
		return errors.New("session history is nil")
	}
	if err := value.Validate(); err != nil {
		return fmt.Errorf("session history append: %w", err)
	}
	history.mutex.Lock()
	defer history.mutex.Unlock()
	if value.ID != history.session.ID {
		return errors.New("session history append changed session ID")
	}
	combined := append(cloneItems(history.items), cloneItems(items)...)
	if err := validateHistoryItems(value.ID, combined); err != nil {
		return err
	}
	history.session = value
	history.items = combined
	history.revision++
	return nil
}

func validateHistoryItems(sessionID SessionID, items []RolloutItem) error {
	var previous int64
	seen := make(map[RolloutItemID]struct{}, len(items))
	for index, item := range items {
		if err := item.Validate(); err != nil {
			return fmt.Errorf("session history item %d: %w", index, err)
		}
		if item.SessionID != sessionID {
			return fmt.Errorf("session history item %d belongs to session %q", index, item.SessionID)
		}
		if index > 0 && item.Sequence != previous+1 {
			return fmt.Errorf("session history item %d sequence %d does not follow %d", index, item.Sequence, previous)
		}
		if _, duplicate := seen[item.ID]; duplicate {
			return fmt.Errorf("session history contains duplicate item %q", item.ID)
		}
		seen[item.ID] = struct{}{}
		previous = item.Sequence
	}
	return nil
}
