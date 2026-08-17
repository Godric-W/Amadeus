package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

type ModelMessages struct {
	InstructionsTemplate  string                     `json:"instructions_template,omitempty"`
	InstructionsVariables ModelInstructionsVariables `json:"instructions_variables,omitempty"`
	CollaborationModes    CollaborationModeMessages  `json:"collaboration_modes,omitempty"`
	SummarizationPrompt   string                     `json:"summarization_prompt,omitempty"`
	SummaryPrefix         string                     `json:"summary_prefix,omitempty"`
	Revision              string                     `json:"revision,omitempty"`
	Source                string                     `json:"source,omitempty"`
}

type ModelInstructionsVariables struct {
	PersonalityDefault   string `json:"personality_default,omitempty"`
	PersonalityFriendly  string `json:"personality_friendly,omitempty"`
	PersonalityPragmatic string `json:"personality_pragmatic,omitempty"`
}

type CollaborationModeMessages struct {
	Default string `json:"default,omitempty"`
	Plan    string `json:"plan,omitempty"`
}

func (messages ModelMessages) ResolveBaseInstructions(personality string) (BaseInstructions, error) {
	template := strings.TrimSpace(messages.InstructionsTemplate)
	if template == "" {
		return BaseInstructions{}, errors.New("model instructions template is empty")
	}
	personalityText := messages.InstructionsVariables.PersonalityDefault
	switch strings.TrimSpace(personality) {
	case "":
	case "friendly":
		personalityText = messages.InstructionsVariables.PersonalityFriendly
	case "pragmatic":
		personalityText = messages.InstructionsVariables.PersonalityPragmatic
	default:
		return BaseInstructions{}, errors.New("unsupported model personality")
	}
	return BaseInstructions{Text: strings.ReplaceAll(template, "{{ personality }}", personalityText)}, nil
}

func (messages ModelMessages) HasInstructions() bool {
	return strings.TrimSpace(messages.InstructionsTemplate) != ""
}

func (messages ModelMessages) HasCompaction() bool {
	return strings.TrimSpace(messages.SummarizationPrompt) != "" && strings.TrimSpace(messages.SummaryPrefix) != ""
}

func (messages ModelMessages) RevisionID() string {
	if strings.TrimSpace(messages.Revision) != "" {
		return messages.Revision
	}
	encoded, _ := json.Marshal(messages)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func (messages ModelMessages) Normalized() ModelMessages {
	if strings.TrimSpace(messages.Revision) == "" {
		messages.Revision = messages.RevisionID()
	}
	if strings.TrimSpace(messages.Source) == "" {
		messages.Source = "model"
	}
	return messages
}
