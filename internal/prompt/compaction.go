package prompt

import (
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
)

func CompactionMessages(messages llm.ModelMessages) (llm.BaseInstructions, string, error) {
	if !messages.HasCompaction() {
		return llm.BaseInstructions{}, "", errors.New("compaction prompt assets are incomplete")
	}
	return llm.BaseInstructions{Text: strings.TrimSpace(messages.SummarizationPrompt)}, strings.TrimSpace(messages.SummaryPrefix), nil
}
