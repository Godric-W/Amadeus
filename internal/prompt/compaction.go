package prompt

import (
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/prompt/builtin"
)

type CompactionAssets struct {
	SummarizationPrompt   string
	SummaryPrefix         string
	SummarizationRevision string
	SummaryPrefixRevision string
	Source                string
}

func (assets CompactionAssets) Valid() bool {
	return strings.TrimSpace(assets.SummarizationPrompt) != "" &&
		strings.TrimSpace(assets.SummaryPrefix) != "" &&
		strings.TrimSpace(assets.SummarizationRevision) != "" &&
		strings.TrimSpace(assets.SummaryPrefixRevision) != ""
}

func LoadCompactionAssets() (CompactionAssets, error) {
	promptText, err := builtin.Read(builtin.ContextCompaction)
	if err != nil {
		return CompactionAssets{}, err
	}
	prefix, err := builtin.Read(builtin.ContextCompactionPrefix)
	if err != nil {
		return CompactionAssets{}, err
	}
	assets := CompactionAssets{
		SummarizationPrompt:   strings.TrimSpace(promptText),
		SummaryPrefix:         strings.TrimSpace(prefix),
		SummarizationRevision: builtin.RevisionFor([]builtin.ID{builtin.ContextCompaction}),
		SummaryPrefixRevision: builtin.RevisionFor([]builtin.ID{builtin.ContextCompactionPrefix}),
		Source:                "amadeus.builtin.compaction",
	}
	if !assets.Valid() {
		return CompactionAssets{}, errors.New("compaction prompt assets are incomplete")
	}
	return assets, nil
}
