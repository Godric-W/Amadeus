package config

import "time"

type APIMode string

const (
	APIResponses       APIMode = "responses"
	APIChatCompletions APIMode = "chat_completions"
)

type ProviderDialect string

const (
	DialectStandard ProviderDialect = "standard"
	DialectOpenAI   ProviderDialect = "openai"
	DialectDeepSeek ProviderDialect = "deepseek"
	DialectQwen     ProviderDialect = "qwen"
	DialectGLM      ProviderDialect = "glm"
)

type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

type WebSearchProvider string

const (
	WebSearchDuckDuckGo WebSearchProvider = "duckduckgo"
	WebSearchTavily     WebSearchProvider = "tavily"
	WebSearchSearXNG    WebSearchProvider = "searxng"
	WebSearchBrave      WebSearchProvider = "brave"
)

type Config struct {
	Version         int                       `yaml:"version"`
	DefaultProvider string                    `yaml:"default_provider"`
	Providers       map[string]ProviderConfig `yaml:"providers"`
	Agent           AgentConfig               `yaml:"agent"`
	Web             WebConfig                 `yaml:"web"`
	Logging         LoggingConfig             `yaml:"logging"`
}

type WebConfig struct {
	Fetch  WebFetchConfig  `yaml:"fetch"`
	Search WebSearchConfig `yaml:"search"`
}

type WebFetchConfig struct {
	Enabled      bool          `yaml:"enabled"`
	Timeout      time.Duration `yaml:"timeout"`
	MaxBytes     int64         `yaml:"max_bytes"`
	MaxRedirects int           `yaml:"max_redirects"`
}

type WebSearchConfig struct {
	Enabled    bool              `yaml:"enabled"`
	Provider   WebSearchProvider `yaml:"provider"`
	APIKey     string            `yaml:"api_key"`
	BaseURL    string            `yaml:"base_url"`
	Timeout    time.Duration     `yaml:"timeout"`
	MaxResults int               `yaml:"max_results"`
}

type ProviderConfig struct {
	API             APIMode         `yaml:"api"`
	Dialect         ProviderDialect `yaml:"dialect"`
	APIKey          string          `yaml:"api_key"`
	BaseURL         string          `yaml:"base_url"`
	Model           string          `yaml:"model"`
	Timeout         time.Duration   `yaml:"timeout"`
	MaxRetries      int             `yaml:"max_retries"`
	Temperature     float64         `yaml:"temperature"`
	MaxOutputTokens int             `yaml:"max_output_tokens"`
	ContextWindow   int64           `yaml:"context_window"`
}

type AgentConfig struct {
	MaxIterations    int           `yaml:"max_iterations"`
	MaxToolCalls     int           `yaml:"max_tool_calls"`
	MaxInputTokens   int64         `yaml:"max_input_tokens"`
	MaxOutputTokens  int64         `yaml:"max_output_tokens"`
	MaxDuration      time.Duration `yaml:"max_duration"`
	MaxParallelTools int           `yaml:"max_parallel_tools"`
}

type LoggingConfig struct {
	Level    LogLevel `yaml:"level"`
	TraceLLM bool     `yaml:"trace_llm"`
}
