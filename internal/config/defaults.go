package config

import "time"

const (
	CurrentVersion      = 1
	DefaultProviderName = "openai"
)

func Default() Config {
	openAI := defaultProviderConfig()
	openAI.API = APIResponses
	openAI.Dialect = DialectOpenAI
	openAI.BaseURL = "https://api.openai.com/v1"

	return Config{
		Version:         CurrentVersion,
		DefaultProvider: DefaultProviderName,
		Providers: map[string]ProviderConfig{
			DefaultProviderName: openAI,
		},
		Agent: AgentConfig{
			MaxParallelTools: 4,
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

func defaultProviderConfig() ProviderConfig {
	return ProviderConfig{
		Dialect:             DialectStandard,
		Timeout:             2 * time.Minute,
		RequestMaxRetries:   4,
		StreamMaxRetries:    5,
		StreamIdleTimeout:   5 * time.Minute,
		Temperature:         0.2,
		MaxOutputTokens:     8192,
		ContextWindow:       128_000,
		ToolOutputMaxTokens: 16_384,
	}
}
