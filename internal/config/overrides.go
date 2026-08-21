package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
)

const (
	EnvModelProvider                    = "AMADEUS_MODEL_PROVIDER"
	EnvWireAPI                          = "AMADEUS_WIRE_API"
	EnvDialect                          = "AMADEUS_DIALECT"
	EnvAPIKey                           = "AMADEUS_API_KEY"
	EnvBaseURL                          = "AMADEUS_BASE_URL"
	EnvModel                            = "AMADEUS_MODEL"
	EnvModelReasoningEffort             = "AMADEUS_MODEL_REASONING_EFFORT"
	EnvModelInputModalities             = "AMADEUS_MODEL_INPUT_MODALITIES"
	EnvModelSupportsOriginalImageDetail = "AMADEUS_MODEL_SUPPORTS_ORIGINAL_IMAGE_DETAIL"
)

type Overrides struct {
	ModelProvider                    *string
	WireAPI                          *WireAPI
	Dialect                          *ProviderDialect
	APIKey                           *string
	BaseURL                          *string
	Model                            *string
	ModelReasoningEffort             *llm.ReasoningEffort
	ModelInputModalities             *[]llm.InputModality
	ModelSupportsOriginalImageDetail *bool
}

func applyEnvironmentOverrides(configured Config, lookup EnvLookup) (Config, error) {
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
	if value, ok := lookup(EnvModelInputModalities); ok {
		modalities := parseInputModalities(value)
		overrides.ModelInputModalities = &modalities
	}
	if value, ok := lookup(EnvModelSupportsOriginalImageDetail); ok {
		supported, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return Config{}, fmt.Errorf("%s must be a boolean: %w", EnvModelSupportsOriginalImageDetail, err)
		}
		overrides.ModelSupportsOriginalImageDetail = &supported
	}

	return ApplyOverrides(configured, overrides), nil
}

func ApplyOverrides(configured Config, overrides Overrides) Config {
	overridden := clone(configured)
	assign(&overridden.ModelProvider, overrides.ModelProvider)
	assign(&overridden.Model, overrides.Model)
	if overrides.ModelReasoningEffort != nil {
		overridden.ModelReasoningEffort = llm.CloneReasoningEffort(overrides.ModelReasoningEffort)
	}
	if overrides.ModelInputModalities != nil {
		overridden.ModelInputModalities = append([]llm.InputModality(nil), (*overrides.ModelInputModalities)...)
	}
	assign(&overridden.ModelSupportsOriginalImageDetail, overrides.ModelSupportsOriginalImageDetail)

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

func parseInputModalities(value string) []llm.InputModality {
	fields := strings.Split(value, ",")
	modalities := make([]llm.InputModality, 0, len(fields))
	for _, field := range fields {
		if modality := llm.InputModality(strings.TrimSpace(field)); modality != "" {
			modalities = append(modalities, modality)
		}
	}
	return modalities
}
