package config

import (
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
)

const (
	DefaultToolOutputTokenLimit = int64(10_000)
)

func Default() Config {
	return Config{
		ModelInputModalities: []llm.InputModality{llm.InputModalityText},
		ToolOutputTokenLimit: DefaultToolOutputTokenLimit,
		ModelProviders:       make(map[string]ModelProviderInfo),
		Features:             FeatureConfig{Goals: true},
		Agent: AgentConfig{
			MaxParallelTools: 4,
			MultiAgent: MultiAgentConfig{
				Enabled: true, MaxAgents: 4, MaxDepth: 1,
				ChildMaxSamples: 20, ChildMaxToolCalls: 100, ChildMaxDuration: 15 * time.Minute,
			},
		},
		Web: WebConfig{
			Fetch:  WebFetchConfig{Timeout: 30 * time.Second, MaxBytes: 1 << 20, MaxRedirects: 3},
			Search: WebSearchConfig{Provider: WebSearchDuckDuckGo, Timeout: 15 * time.Second, MaxResults: 5},
		},
		Logging: LoggingConfig{
			Level: LogLevelInfo,
		},
	}
}

func defaultModelProviderInfo() ModelProviderInfo {
	return ModelProviderInfo{
		WireAPI:           WireAPIResponses,
		Dialect:           DialectStandard,
		Timeout:           2 * time.Minute,
		RequestMaxRetries: 4,
		StreamMaxRetries:  5,
		StreamIdleTimeout: 5 * time.Minute,
	}
}
