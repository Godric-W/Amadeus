package config

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	maxProviderTimeout = 30 * time.Minute
	maxProviderRetries = 100
	maxContextWindow   = int64(100_000_000)
	maxToolOutputTokens = int64(1_000_000)
	maxParallelTools   = 64
	maxWebTimeout      = 2 * time.Minute
	maxWebBytes        = int64(16 << 20)
	maxWebResults      = 10
)

type ValidationIssue struct {
	Path    string
	Message string
}

type ValidationError struct {
	Issues []ValidationIssue
}

func (validationError *ValidationError) Error() string {
	var builder strings.Builder
	builder.WriteString("configuration validation failed")
	for _, issue := range validationError.Issues {
		builder.WriteString("\n- ")
		builder.WriteString(issue.Path)
		builder.WriteString(": ")
		builder.WriteString(issue.Message)
	}

	return builder.String()
}

func Validate(configured Config) error {
	issues := make([]ValidationIssue, 0)
	addIssue := func(path, message string) {
		issues = append(issues, ValidationIssue{Path: path, Message: message})
	}

	if configured.Version != CurrentVersion {
		addIssue("version", fmt.Sprintf("must be %d", CurrentVersion))
	}

	if strings.TrimSpace(configured.Model) == "" {
		addIssue("model", "must not be empty")
	}
	modelProvider := strings.TrimSpace(configured.ModelProvider)
	if modelProvider == "" {
		addIssue("model_provider", "must not be empty")
	}
	if configured.ModelContextWindow <= 0 || configured.ModelContextWindow > maxContextWindow {
		addIssue("model_context_window", fmt.Sprintf("must be greater than 0 and at most %d", maxContextWindow))
	}
	derivedCompactLimit := configured.ModelContextWindow * 9 / 10
	if configured.ModelAutoCompactTokenLimit < 0 || configured.ModelAutoCompactTokenLimit > derivedCompactLimit {
		addIssue("model_auto_compact_token_limit", "must be zero (derived) or no greater than 90% of model_context_window")
	}
	if configured.ToolOutputTokenLimit <= 0 || configured.ToolOutputTokenLimit > maxToolOutputTokens {
		addIssue("tool_output_token_limit", fmt.Sprintf("must be greater than 0 and at most %d", maxToolOutputTokens))
	}
	if len(configured.ModelProviders) == 0 {
		addIssue("model_providers", "must contain at least one provider")
	} else if modelProvider != "" {
		if _, ok := configured.ModelProviders[configured.ModelProvider]; !ok {
			addIssue("model_provider", fmt.Sprintf("provider %q is not configured", configured.ModelProvider))
		}
	}

	providerNames := make([]string, 0, len(configured.ModelProviders))
	for name := range configured.ModelProviders {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)
	for _, name := range providerNames {
		validateProvider(name, configured.ModelProviders[name], addIssue)
	}

	validateAgent(configured.Agent, addIssue)
	validateWeb(configured.Web, addIssue)
	validateLogging(configured.Logging, addIssue)

	if len(issues) == 0 {
		return nil
	}

	return &ValidationError{Issues: issues}
}

func validateWeb(configured WebConfig, addIssue func(string, string)) {
	if configured.Fetch.Timeout <= 0 || configured.Fetch.Timeout > maxWebTimeout {
		addIssue("web.fetch.timeout", fmt.Sprintf("must be greater than 0 and at most %s", maxWebTimeout))
	}
	if configured.Fetch.MaxBytes <= 0 || configured.Fetch.MaxBytes > maxWebBytes {
		addIssue("web.fetch.max_bytes", fmt.Sprintf("must be greater than 0 and at most %d", maxWebBytes))
	}
	if configured.Fetch.MaxRedirects < 0 || configured.Fetch.MaxRedirects > 10 {
		addIssue("web.fetch.max_redirects", "must be between 0 and 10")
	}
	if configured.Search.Timeout <= 0 || configured.Search.Timeout > maxWebTimeout {
		addIssue("web.search.timeout", fmt.Sprintf("must be greater than 0 and at most %s", maxWebTimeout))
	}
	if configured.Search.MaxResults <= 0 || configured.Search.MaxResults > maxWebResults {
		addIssue("web.search.max_results", fmt.Sprintf("must be greater than 0 and at most %d", maxWebResults))
	}
	if !configured.Search.Enabled {
		return
	}
	switch configured.Search.Provider {
	case WebSearchDuckDuckGo:
	case WebSearchTavily, WebSearchBrave:
		if strings.TrimSpace(configured.Search.APIKey) == "" {
			addIssue("web.search.api_key", "must not be empty for the selected provider")
		}
	case WebSearchSearXNG:
		if strings.TrimSpace(configured.Search.BaseURL) == "" {
			addIssue("web.search.base_url", "must not be empty for searxng")
		}
	default:
		addIssue("web.search.provider", fmt.Sprintf("must be %q, %q, %q, or %q", WebSearchDuckDuckGo, WebSearchTavily, WebSearchSearXNG, WebSearchBrave))
	}
	if strings.TrimSpace(configured.Search.BaseURL) != "" {
		validateBaseURL("web.search.base_url", configured.Search.BaseURL, addIssue)
	}
}

func validateProvider(name string, provider ModelProviderInfo, addIssue func(string, string)) {
	path := "model_providers." + name
	if strings.TrimSpace(name) == "" {
		addIssue("model_providers", "provider name must not be empty")
	}

	switch provider.WireAPI {
	case WireAPIResponses, WireAPIChatCompletions:
	default:
		addIssue(path+".wire_api", fmt.Sprintf("must be %q or %q", WireAPIResponses, WireAPIChatCompletions))
	}

	switch provider.Dialect {
	case DialectStandard, DialectOpenAI, DialectDeepSeek, DialectQwen, DialectGLM:
	default:
		addIssue(path+".dialect", fmt.Sprintf(
			"must be %q, %q, %q, %q, or %q",
			DialectStandard,
			DialectOpenAI,
			DialectDeepSeek,
			DialectQwen,
			DialectGLM,
		))
	}

	validateBaseURL(path+".base_url", provider.BaseURL, addIssue)

	if provider.Timeout <= 0 || provider.Timeout > maxProviderTimeout {
		addIssue(path+".timeout", fmt.Sprintf("must be greater than 0 and at most %s", maxProviderTimeout))
	}
	if provider.RequestMaxRetries < 0 || provider.RequestMaxRetries > maxProviderRetries {
		addIssue(path+".request_max_retries", fmt.Sprintf("must be between 0 and %d", maxProviderRetries))
	}
	if provider.StreamMaxRetries < 0 || provider.StreamMaxRetries > maxProviderRetries {
		addIssue(path+".stream_max_retries", fmt.Sprintf("must be between 0 and %d", maxProviderRetries))
	}
	if provider.StreamIdleTimeout <= 0 {
		addIssue(path+".stream_idle_timeout", "must be greater than 0")
	}
}

func validateBaseURL(path, value string, addIssue func(string, string)) {
	if strings.TrimSpace(value) == "" {
		addIssue(path, "must not be empty")
		return
	}

	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		addIssue(path, "must be a valid absolute HTTP or HTTPS URL")
		return
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		addIssue(path, "scheme must be http or https")
	}
	if parsed.User != nil {
		addIssue(path, "must not contain user information")
	}
	if parsed.Fragment != "" {
		addIssue(path, "must not contain a fragment")
	}
}

func validateAgent(agent AgentConfig, addIssue func(string, string)) {
	if agent.MaxParallelTools <= 0 || agent.MaxParallelTools > maxParallelTools {
		addIssue("agent.max_parallel_tools", fmt.Sprintf("must be greater than 0 and at most %d", maxParallelTools))
	}
}

func validateLogging(logging LoggingConfig, addIssue func(string, string)) {
	switch logging.Level {
	case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
	default:
		addIssue("logging.level", fmt.Sprintf("must be %q, %q, %q, or %q", LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError))
	}
}
