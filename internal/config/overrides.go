package config

import "github.com/Godric-W/Amadeus/internal/llm"

const (
	EnvModelProvider        = "AMADEUS_MODEL_PROVIDER"
	EnvWireAPI              = "AMADEUS_WIRE_API"
	EnvDialect              = "AMADEUS_DIALECT"
	EnvAPIKey               = "AMADEUS_API_KEY"
	EnvBaseURL              = "AMADEUS_BASE_URL"
	EnvModel                = "AMADEUS_MODEL"
	EnvModelReasoningEffort = "AMADEUS_MODEL_REASONING_EFFORT"
)

type Overrides struct {
	ModelProvider        *string
	WireAPI              *WireAPI
	Dialect              *ProviderDialect
	APIKey               *string
	BaseURL              *string
	Model                *string
	ModelReasoningEffort *llm.ReasoningEffort
}

func applyEnvironmentOverrides(configured Config, lookup EnvLookup) Config {
	var overrides Overrides
	if value, ok := lookup(EnvModelProvider); ok {
		overrides.ModelProvider = &value
	}
	if value, ok := lookup(EnvWireAPI); ok {
		wireAPI := WireAPI(value)
		overrides.WireAPI = &wireAPI
	}
	if value, ok := lookup(EnvDialect); ok {
		dialect := ProviderDialect(value)
		overrides.Dialect = &dialect
	}
	if value, ok := lookup(EnvAPIKey); ok {
		overrides.APIKey = &value
	}
	if value, ok := lookup(EnvBaseURL); ok {
		overrides.BaseURL = &value
	}
	if value, ok := lookup(EnvModel); ok {
		overrides.Model = &value
	}
	if value, ok := lookup(EnvModelReasoningEffort); ok {
		effort := llm.ReasoningEffort(value)
		overrides.ModelReasoningEffort = &effort
	}

	return ApplyOverrides(configured, overrides)
}

func ApplyOverrides(configured Config, overrides Overrides) Config {
	overridden := clone(configured)
	assign(&overridden.ModelProvider, overrides.ModelProvider)
	assign(&overridden.Model, overrides.Model)
	if overrides.ModelReasoningEffort != nil {
		overridden.ModelReasoningEffort = llm.CloneReasoningEffort(overrides.ModelReasoningEffort)
	}

	providerName := overridden.ModelProvider
	provider, exists := overridden.ModelProviders[providerName]
	if !exists {
		provider = defaultModelProviderInfo()
	}
	changed := overrides.ModelProvider != nil
	if overrides.WireAPI != nil {
		provider.WireAPI = *overrides.WireAPI
		changed = true
	}
	if overrides.Dialect != nil {
		provider.Dialect = *overrides.Dialect
		changed = true
	}
	if overrides.APIKey != nil {
		provider.APIKey = *overrides.APIKey
		changed = true
	}
	if overrides.BaseURL != nil {
		provider.BaseURL = *overrides.BaseURL
		changed = true
	}
	if changed && providerName != "" {
		overridden.ModelProviders[providerName] = provider
	}

	return overridden
}
