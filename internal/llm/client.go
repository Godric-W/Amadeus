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
