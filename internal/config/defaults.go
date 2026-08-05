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
			MaxIterations:    30,
			MaxToolCalls:     120,
			MaxInputTokens:   1_000_000,
			MaxOutputTokens:  245_760,
			MaxDuration:      30 * time.Minute,
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
		Dialect:         DialectStandard,
		Timeout:         2 * time.Minute,
		MaxRetries:      2,
		Temperature:     0.2,
		MaxOutputTokens: 8192,
		ContextWindow:   128_000,
	}
}
