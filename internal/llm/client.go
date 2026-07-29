package llm

import "context"

type Client interface {
	Complete(context.Context, Request) (Response, error)
	Stream(context.Context, Request) (Stream, error)
	Model() ModelInfo
	Capabilities() Capabilities
}

type ModelInfo struct {
	Provider string
	Name     string
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
