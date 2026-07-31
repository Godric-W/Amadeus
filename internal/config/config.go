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

type ApprovalDefault string

const (
	ApprovalAsk   ApprovalDefault = "ask"
	ApprovalAllow ApprovalDefault = "allow"
	ApprovalDeny  ApprovalDefault = "deny"
)

type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

type Config struct {
	Version         int                       `yaml:"version"`
	DefaultProvider string                    `yaml:"default_provider"`
	Providers       map[string]ProviderConfig `yaml:"providers"`
	Agent           AgentConfig               `yaml:"agent"`
	Approval        ApprovalConfig            `yaml:"approval"`
	Logging         LoggingConfig             `yaml:"logging"`
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
}

type AgentConfig struct {
	MaxSteps         int           `yaml:"max_steps"`
	MaxToolCalls     int           `yaml:"max_tool_calls"`
	MaxInputTokens   int64         `yaml:"max_input_tokens"`
	MaxOutputTokens  int64         `yaml:"max_output_tokens"`
	MaxDuration      time.Duration `yaml:"max_duration"`
	MaxParallelTools int           `yaml:"max_parallel_tools"`
}

type ApprovalConfig struct {
	Enabled bool            `yaml:"enabled"`
	Default ApprovalDefault `yaml:"default"`
}

type LoggingConfig struct {
	Level    LogLevel `yaml:"level"`
	TraceLLM bool     `yaml:"trace_llm"`
}
