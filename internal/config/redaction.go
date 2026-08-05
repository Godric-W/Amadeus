package config

const RedactedSecret = "[REDACTED]"

func Redact(configured Config) Config {
	redacted := clone(configured)
	for name, provider := range redacted.Providers {
		if provider.APIKey != "" {
			provider.APIKey = RedactedSecret
			redacted.Providers[name] = provider
		}
	}
	if redacted.Web.Search.APIKey != "" {
		redacted.Web.Search.APIKey = RedactedSecret
	}

	return redacted
}
