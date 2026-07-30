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
			Mode:             AgentModeReact,
			MaxSteps:         30,
			MaxParallelTools: 4,
		},
		Approval: ApprovalConfig{
			Enabled: true,
			Default: ApprovalAsk,
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
	}
}
