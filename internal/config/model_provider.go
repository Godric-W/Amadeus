package config

import "time"

type WireAPI string

const (
	WireAPIResponses       WireAPI = "responses"
	WireAPIChatCompletions WireAPI = "chat_completions"
)

type ProviderDialect string

const (
	DialectStandard ProviderDialect = "standard"
	DialectOpenAI   ProviderDialect = "openai"
	DialectDeepSeek ProviderDialect = "deepseek"
	DialectQwen     ProviderDialect = "qwen"
	DialectGLM      ProviderDialect = "glm"
)

type ModelProviderInfo struct {
	WireAPI             WireAPI         `yaml:"wire_api"`
	Dialect             ProviderDialect `yaml:"dialect"`
	APIKey              string          `yaml:"api_key"`
	BaseURL             string          `yaml:"base_url"`
	Timeout             time.Duration   `yaml:"timeout"`
	RequestMaxRetries   int             `yaml:"request_max_retries"`
	StreamMaxRetries    int             `yaml:"stream_max_retries"`
	StreamIdleTimeout   time.Duration   `yaml:"stream_idle_timeout"`
}
