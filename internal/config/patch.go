package config

import "time"

type configPatch struct {
	Version         *int                     `yaml:"version"`
	DefaultProvider *string                  `yaml:"default_provider"`
	Providers       map[string]providerPatch `yaml:"providers"`
	Agent           *agentPatch              `yaml:"agent"`
	Web             *webPatch                `yaml:"web"`
	Logging         *loggingPatch            `yaml:"logging"`
}

type webPatch struct {
	Fetch  *webFetchPatch  `yaml:"fetch"`
	Search *webSearchPatch `yaml:"search"`
}

type webFetchPatch struct {
	Enabled      *bool          `yaml:"enabled"`
	Timeout      *time.Duration `yaml:"timeout"`
	MaxBytes     *int64         `yaml:"max_bytes"`
	MaxRedirects *int           `yaml:"max_redirects"`
}

type webSearchPatch struct {
	Enabled    *bool              `yaml:"enabled"`
	Provider   *WebSearchProvider `yaml:"provider"`
	APIKey     *string            `yaml:"api_key"`
	BaseURL    *string            `yaml:"base_url"`
	Timeout    *time.Duration     `yaml:"timeout"`
	MaxResults *int               `yaml:"max_results"`
}

type providerPatch struct {
	API             *APIMode         `yaml:"api"`
	Dialect         *ProviderDialect `yaml:"dialect"`
	APIKey          *string          `yaml:"api_key"`
	BaseURL         *string          `yaml:"base_url"`
	Model           *string          `yaml:"model"`
	Timeout         *time.Duration   `yaml:"timeout"`
	MaxRetries      *int             `yaml:"max_retries"`
	Temperature     *float64         `yaml:"temperature"`
	MaxOutputTokens *int             `yaml:"max_output_tokens"`
	ContextWindow   *int64           `yaml:"context_window"`
}

type agentPatch struct {
	MaxIterations    *int           `yaml:"max_iterations"`
	MaxToolCalls     *int           `yaml:"max_tool_calls"`
	MaxInputTokens   *int64         `yaml:"max_input_tokens"`
	MaxOutputTokens  *int64         `yaml:"max_output_tokens"`
	MaxDuration      *time.Duration `yaml:"max_duration"`
	MaxParallelTools *int           `yaml:"max_parallel_tools"`
}

type loggingPatch struct {
	Level    *LogLevel `yaml:"level"`
	TraceLLM *bool     `yaml:"trace_llm"`
}

func (patch configPatch) apply(base Config) Config {
	configured := clone(base)

	assign(&configured.Version, patch.Version)
	assign(&configured.DefaultProvider, patch.DefaultProvider)

	for name, providerPatch := range patch.Providers {
		provider, ok := configured.Providers[name]
		if !ok {
			provider = defaultProviderConfig()
		}
		providerPatch.apply(&provider)
		configured.Providers[name] = provider
	}

	if patch.Agent != nil {
		patch.Agent.apply(&configured.Agent)
	}
	if patch.Web != nil {
		patch.Web.apply(&configured.Web)
	}
	if patch.Logging != nil {
		patch.Logging.apply(&configured.Logging)
	}

	return configured
}

func (patch webPatch) apply(configured *WebConfig) {
	if patch.Fetch != nil {
		assign(&configured.Fetch.Enabled, patch.Fetch.Enabled)
		assign(&configured.Fetch.Timeout, patch.Fetch.Timeout)
		assign(&configured.Fetch.MaxBytes, patch.Fetch.MaxBytes)
		assign(&configured.Fetch.MaxRedirects, patch.Fetch.MaxRedirects)
	}
	if patch.Search != nil {
		assign(&configured.Search.Enabled, patch.Search.Enabled)
		assign(&configured.Search.Provider, patch.Search.Provider)
		assign(&configured.Search.APIKey, patch.Search.APIKey)
		assign(&configured.Search.BaseURL, patch.Search.BaseURL)
		assign(&configured.Search.Timeout, patch.Search.Timeout)
		assign(&configured.Search.MaxResults, patch.Search.MaxResults)
	}
}

func (patch providerPatch) apply(provider *ProviderConfig) {
	assign(&provider.API, patch.API)
	assign(&provider.Dialect, patch.Dialect)
	assign(&provider.APIKey, patch.APIKey)
	assign(&provider.BaseURL, patch.BaseURL)
	assign(&provider.Model, patch.Model)
	assign(&provider.Timeout, patch.Timeout)
	assign(&provider.MaxRetries, patch.MaxRetries)
	assign(&provider.Temperature, patch.Temperature)
	assign(&provider.MaxOutputTokens, patch.MaxOutputTokens)
	assign(&provider.ContextWindow, patch.ContextWindow)
}

func (patch agentPatch) apply(agent *AgentConfig) {
	assign(&agent.MaxIterations, patch.MaxIterations)
	assign(&agent.MaxToolCalls, patch.MaxToolCalls)
	assign(&agent.MaxInputTokens, patch.MaxInputTokens)
	assign(&agent.MaxOutputTokens, patch.MaxOutputTokens)
	assign(&agent.MaxDuration, patch.MaxDuration)
	assign(&agent.MaxParallelTools, patch.MaxParallelTools)
}

func (patch loggingPatch) apply(logging *LoggingConfig) {
	assign(&logging.Level, patch.Level)
	assign(&logging.TraceLLM, patch.TraceLLM)
}

func clone(configured Config) Config {
	cloned := configured
	cloned.Providers = make(map[string]ProviderConfig, len(configured.Providers))
	for name, provider := range configured.Providers {
		cloned.Providers[name] = provider
	}
	return cloned
}

func assign[T any](target *T, value *T) {
	if value != nil {
		*target = *value
	}
}
