package session

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SummaryID string

type ConversationSummary struct {
	ID                  SummaryID             `json:"id"`
	SessionID           ConversationSessionID `json:"session_id"`
	FromMessageSequence int64                 `json:"from_message_sequence"`
	ToMessageSequence   int64                 `json:"to_message_sequence"`
	Content             string                `json:"content"`
	SourceHash          string                `json:"source_hash"`
	SummaryHash         string                `json:"summary_hash"`
	Provider            string                `json:"provider,omitempty"`
	Model               string                `json:"model,omitempty"`
	CreatedAt           time.Time             `json:"created_at"`
}

func NewConversationSummary(id SummaryID, sessionID ConversationSessionID, from, to int64, content, sourceHash, provider, model string, createdAt time.Time) (ConversationSummary, error) {
	content = strings.TrimSpace(content)
	digest := sha256.Sum256([]byte(content))
	summary := ConversationSummary{
		ID: id, SessionID: sessionID, FromMessageSequence: from, ToMessageSequence: to,
		Content: content, SourceHash: strings.ToLower(strings.TrimSpace(sourceHash)), SummaryHash: hex.EncodeToString(digest[:]),
		Provider: strings.TrimSpace(provider), Model: strings.TrimSpace(model), CreatedAt: createdAt.UTC(),
	}
	return summary, summary.Validate()
}

func (summary ConversationSummary) Validate() error {
	if err := validateID("conversation summary", string(summary.ID)); err != nil {
		return err
	}
	if err := validateID("conversation summary session", string(summary.SessionID)); err != nil {
		return err
	}
	if summary.FromMessageSequence < 1 || summary.ToMessageSequence < summary.FromMessageSequence {
		return errors.New("conversation summary message range is invalid")
	}
	if strings.TrimSpace(summary.Content) == "" {
		return errors.New("conversation summary content is empty")
	}
	if !validSHA256(summary.SourceHash) || !validSHA256(summary.SummaryHash) {
		return errors.New("conversation summary hash is invalid")
	}
	digest := sha256.Sum256([]byte(summary.Content))
	if hex.EncodeToString(digest[:]) != summary.SummaryHash {
		return errors.New("conversation summary hash does not match content")
	}
	if summary.CreatedAt.IsZero() {
		return errors.New("conversation summary created_at is zero")
	}
	return nil
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func ConversationSourceHash(messages []Message) (string, error) {
	if len(messages) == 0 {
		return "", errors.New("conversation summary source messages are empty")
	}
	hash := sha256.New()
	for _, message := range messages {
		if err := message.Validate(); err != nil {
			return "", fmt.Errorf("validate conversation summary source: %w", err)
		}
		fmt.Fprintf(hash, "%d\x00%s\x00%s\x00", message.Sequence, message.Role, message.Content)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
