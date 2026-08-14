package llm

import "context"

type Client interface {
	Complete(context.Context, Request) (Response, error)
	Stream(context.Context, Request) (Stream, error)
	Model() ModelInfo
	Capabilities() Capabilities
}

type ModelInfo struct {
	Provider                  string
	Name                      string
	ContextWindow             int64
	AutoCompactTokenLimit     int64
	MaxOutputTokens           int
	SupportsParallelToolCalls bool
	ToolOutputMaxTokens       int64
	InputModalities           []InputModality
}

type InputModality string

const (
	InputModalityText  InputModality = "text"
	InputModalityImage InputModality = "image"
)

func (info ModelInfo) SupportsInput(modality InputModality) bool {
	info = info.Normalized()
	for _, candidate := range info.InputModalities {
		if candidate == modality {
			return true
		}
	}
	return false
}

func (info ModelInfo) Normalized() ModelInfo {
	if info.ContextWindow > 0 {
		defaultLimit := info.ContextWindow * 9 / 10
		if info.AutoCompactTokenLimit <= 0 || info.AutoCompactTokenLimit > defaultLimit {
			info.AutoCompactTokenLimit = defaultLimit
		}
	}
	if info.ToolOutputMaxTokens <= 0 {
		info.ToolOutputMaxTokens = 16_384
	}
	if len(info.InputModalities) == 0 {
		info.InputModalities = []InputModality{InputModalityText}
	} else {
		seen := make(map[InputModality]struct{}, len(info.InputModalities)+1)
		normalized := make([]InputModality, 0, len(info.InputModalities)+1)
		for _, modality := range append([]InputModality{InputModalityText}, info.InputModalities...) {
			if modality != InputModalityText && modality != InputModalityImage {
				continue
			}
			if _, exists := seen[modality]; exists {
				continue
			}
			seen[modality] = struct{}{}
			normalized = append(normalized, modality)
		}
		info.InputModalities = normalized
	}
	return info
}

type Capabilities struct {
	SupportsStreaming          bool
	SupportsDeveloperRole      bool
	SupportsReasoning          bool
	SupportsJSONSchema         bool
	SupportsNamedToolChoice    bool
	SupportsRequiredToolChoice bool
	SupportsParallelToolCalls  bool
	SupportsImages             bool
	SupportsAudio              bool
	SupportsVideo              bool
	SupportsStreamUsage        bool
	SupportsPromptCacheUsage   bool
}
