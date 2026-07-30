package config

import "time"

type configPatch struct {
	Version         *int                     `yaml:"version"`
	DefaultProvider *string                  `yaml:"default_provider"`
	Providers       map[string]providerPatch `yaml:"providers"`
	Agent           *agentPatch              `yaml:"agent"`
	Approval        *approvalPatch           `yaml:"approval"`
	Logging         *loggingPatch            `yaml:"logging"`
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
}

type agentPatch struct {
	Mode             *AgentMode `yaml:"mode"`
	MaxSteps         *int       `yaml:"max_steps"`
	MaxParallelTools *int       `yaml:"max_parallel_tools"`
}

type approvalPatch struct {
	Enabled *bool            `yaml:"enabled"`
	Default *ApprovalDefault `yaml:"default"`
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
	if patch.Approval != nil {
		patch.Approval.apply(&configured.Approval)
	}
	if patch.Logging != nil {
		patch.Logging.apply(&configured.Logging)
	}

	return configured
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
}

func (patch agentPatch) apply(agent *AgentConfig) {
	assign(&agent.Mode, patch.Mode)
	assign(&agent.MaxSteps, patch.MaxSteps)
	assign(&agent.MaxParallelTools, patch.MaxParallelTools)
}

func (patch approvalPatch) apply(approval *ApprovalConfig) {
	assign(&approval.Enabled, patch.Enabled)
	assign(&approval.Default, patch.Default)
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
