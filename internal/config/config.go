package config

import (
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
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
	Model                            string                       `yaml:"model"`
	ModelProvider                    string                       `yaml:"model_provider"`
	ModelContextWindow               int64                        `yaml:"model_context_window"`
	ModelReasoningEffort             *llm.ReasoningEffort         `yaml:"model_reasoning_effort,omitempty"`
	ModelInputModalities             []llm.InputModality          `yaml:"model_input_modalities"`
	ModelSupportsOriginalImageDetail bool                         `yaml:"model_supports_original_image_detail"`
	ModelAutoCompactTokenLimit       int64                        `yaml:"model_auto_compact_token_limit"`
	ToolOutputTokenLimit             int64                        `yaml:"tool_output_token_limit"`
	ModelProviders                   map[string]ModelProviderInfo `yaml:"model_providers"`
	Agent                            AgentConfig                  `yaml:"agent"`
	Web                              WebConfig                    `yaml:"web"`
	Logging                          LoggingConfig                `yaml:"logging"`
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

type AgentConfig struct {
	MaxParallelTools int              `yaml:"max_parallel_tools"`
	MultiAgent       MultiAgentConfig `yaml:"multi_agent"`
}

type MultiAgentConfig struct {
	Enabled           bool          `yaml:"enabled"`
	MaxAgents         int           `yaml:"max_agents"`
	MaxDepth          int           `yaml:"max_depth"`
	ChildMaxSamples   int           `yaml:"child_max_samples"`
	ChildMaxToolCalls int           `yaml:"child_max_tool_calls"`
	ChildMaxDuration  time.Duration `yaml:"child_max_duration"`
}

type LoggingConfig struct {
	Level    LogLevel `yaml:"level"`
	TraceLLM bool     `yaml:"trace_llm"`
}
