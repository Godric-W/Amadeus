package config

import "time"

type configPatch struct {
	Version                    *int                          `yaml:"version"`
	Model                      *string                       `yaml:"model"`
	ModelProvider              *string                       `yaml:"model_provider"`
	ModelContextWindow         *int64                        `yaml:"model_context_window"`
	ModelAutoCompactTokenLimit *int64                        `yaml:"model_auto_compact_token_limit"`
	ToolOutputTokenLimit       *int64                        `yaml:"tool_output_token_limit"`
	ModelProviders             map[string]modelProviderPatch `yaml:"model_providers"`
	Agent                      *agentPatch                   `yaml:"agent"`
	Web                        *webPatch                     `yaml:"web"`
	Logging                    *loggingPatch                 `yaml:"logging"`
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

type modelProviderPatch struct {
	WireAPI           *WireAPI         `yaml:"wire_api"`
	Dialect           *ProviderDialect `yaml:"dialect"`
	APIKey            *string          `yaml:"api_key"`
	BaseURL           *string          `yaml:"base_url"`
	Timeout           *time.Duration   `yaml:"timeout"`
	RequestMaxRetries *int             `yaml:"request_max_retries"`
	StreamMaxRetries  *int             `yaml:"stream_max_retries"`
	StreamIdleTimeout *time.Duration   `yaml:"stream_idle_timeout"`
}

type agentPatch struct {
	MaxParallelTools *int `yaml:"max_parallel_tools"`
}

type loggingPatch struct {
	Level    *LogLevel `yaml:"level"`
	TraceLLM *bool     `yaml:"trace_llm"`
}

func (patch configPatch) apply(base Config) Config {
	configured := clone(base)

	assign(&configured.Version, patch.Version)
	assign(&configured.Model, patch.Model)
	assign(&configured.ModelProvider, patch.ModelProvider)
	assign(&configured.ModelContextWindow, patch.ModelContextWindow)
	assign(&configured.ModelAutoCompactTokenLimit, patch.ModelAutoCompactTokenLimit)
	assign(&configured.ToolOutputTokenLimit, patch.ToolOutputTokenLimit)

	for name, providerPatch := range patch.ModelProviders {
		provider, ok := configured.ModelProviders[name]
		if !ok {
			provider = defaultModelProviderInfo()
		}
		providerPatch.apply(&provider)
		configured.ModelProviders[name] = provider
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

func (patch modelProviderPatch) apply(provider *ModelProviderInfo) {
	assign(&provider.WireAPI, patch.WireAPI)
	assign(&provider.Dialect, patch.Dialect)
	assign(&provider.APIKey, patch.APIKey)
	assign(&provider.BaseURL, patch.BaseURL)
	assign(&provider.Timeout, patch.Timeout)
	assign(&provider.RequestMaxRetries, patch.RequestMaxRetries)
	assign(&provider.StreamMaxRetries, patch.StreamMaxRetries)
	assign(&provider.StreamIdleTimeout, patch.StreamIdleTimeout)
}

func (patch agentPatch) apply(agent *AgentConfig) {
	assign(&agent.MaxParallelTools, patch.MaxParallelTools)
}

func (patch loggingPatch) apply(logging *LoggingConfig) {
	assign(&logging.Level, patch.Level)
	assign(&logging.TraceLLM, patch.TraceLLM)
}

func clone(configured Config) Config {
	cloned := configured
	cloned.ModelProviders = make(map[string]ModelProviderInfo, len(configured.ModelProviders))
	for name, provider := range configured.ModelProviders {
		cloned.ModelProviders[name] = provider
	}
	return cloned
}

func assign[T any](target *T, value *T) {
	if value != nil {
		*target = *value
	}
}
