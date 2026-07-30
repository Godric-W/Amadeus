package config

const (
	EnvProvider = "AMADEUS_PROVIDER"
	EnvAPI      = "AMADEUS_API"
	EnvDialect  = "AMADEUS_DIALECT"
	EnvAPIKey   = "AMADEUS_API_KEY"
	EnvBaseURL  = "AMADEUS_BASE_URL"
	EnvModel    = "AMADEUS_MODEL"
)

type Overrides struct {
	Provider *string
	API      *APIMode
	Dialect  *ProviderDialect
	APIKey   *string
	BaseURL  *string
	Model    *string
}

func applyEnvironmentOverrides(configured Config, lookup EnvLookup) Config {
	var overrides Overrides
	if value, ok := lookup(EnvProvider); ok {
		overrides.Provider = &value
	}
	if value, ok := lookup(EnvAPI); ok {
		api := APIMode(value)
		overrides.API = &api
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

	return ApplyOverrides(configured, overrides)
}

func ApplyOverrides(configured Config, overrides Overrides) Config {
	overridden := clone(configured)
	if overrides.Provider != nil {
		overridden.DefaultProvider = *overrides.Provider
	}

	provider, ok := overridden.Providers[overridden.DefaultProvider]
	if !ok {
		provider = defaultProviderConfig()
	}
	changed := overrides.Provider != nil
	if overrides.API != nil {
		provider.API = *overrides.API
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
	if overrides.Model != nil {
		provider.Model = *overrides.Model
		changed = true
	}
	if changed {
		overridden.Providers[overridden.DefaultProvider] = provider
	}

	return overridden
}
