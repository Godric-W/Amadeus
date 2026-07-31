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
	maxProviderRetries = 10
	maxOutputTokens    = 1_000_000
	maxAgentSteps      = 1_000
	maxAgentToolCalls  = 10_000
	maxAgentTokens     = int64(100_000_000)
	maxAgentDuration   = 24 * time.Hour
	maxParallelTools   = 64
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

	defaultProvider := strings.TrimSpace(configured.DefaultProvider)
	if defaultProvider == "" {
		addIssue("default_provider", "must not be empty")
	}
	if len(configured.Providers) == 0 {
		addIssue("providers", "must contain at least one provider")
	} else if defaultProvider != "" {
		if _, ok := configured.Providers[configured.DefaultProvider]; !ok {
			addIssue("default_provider", fmt.Sprintf("provider %q is not configured", configured.DefaultProvider))
		}
	}

	providerNames := make([]string, 0, len(configured.Providers))
	for name := range configured.Providers {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)
	for _, name := range providerNames {
		validateProvider(name, configured.Providers[name], addIssue)
	}

	validateAgent(configured.Agent, addIssue)
	validateApproval(configured.Approval, addIssue)
	validateLogging(configured.Logging, addIssue)

	if len(issues) == 0 {
		return nil
	}

	return &ValidationError{Issues: issues}
}

func validateProvider(name string, provider ProviderConfig, addIssue func(string, string)) {
	path := "providers." + name
	if strings.TrimSpace(name) == "" {
		addIssue("providers", "provider name must not be empty")
	}

	switch provider.API {
	case APIResponses, APIChatCompletions:
	default:
		addIssue(path+".api", fmt.Sprintf("must be %q or %q", APIResponses, APIChatCompletions))
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
	if provider.MaxRetries < 0 || provider.MaxRetries > maxProviderRetries {
		addIssue(path+".max_retries", fmt.Sprintf("must be between 0 and %d", maxProviderRetries))
	}
	if provider.Temperature < 0 || provider.Temperature > 2 {
		addIssue(path+".temperature", "must be between 0 and 2")
	}
	if provider.MaxOutputTokens <= 0 || provider.MaxOutputTokens > maxOutputTokens {
		addIssue(path+".max_output_tokens", fmt.Sprintf("must be greater than 0 and at most %d", maxOutputTokens))
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
	if agent.MaxSteps <= 0 || agent.MaxSteps > maxAgentSteps {
		addIssue("agent.max_steps", fmt.Sprintf("must be greater than 0 and at most %d", maxAgentSteps))
	}
	if agent.MaxToolCalls <= 0 || agent.MaxToolCalls > maxAgentToolCalls {
		addIssue("agent.max_tool_calls", fmt.Sprintf("must be greater than 0 and at most %d", maxAgentToolCalls))
	}
	if agent.MaxInputTokens <= 0 || agent.MaxInputTokens > maxAgentTokens {
		addIssue("agent.max_input_tokens", fmt.Sprintf("must be greater than 0 and at most %d", maxAgentTokens))
	}
	if agent.MaxOutputTokens <= 0 || agent.MaxOutputTokens > maxAgentTokens {
		addIssue("agent.max_output_tokens", fmt.Sprintf("must be greater than 0 and at most %d", maxAgentTokens))
	}
	if agent.MaxDuration <= 0 || agent.MaxDuration > maxAgentDuration {
		addIssue("agent.max_duration", fmt.Sprintf("must be greater than 0 and at most %s", maxAgentDuration))
	}
	if agent.MaxParallelTools <= 0 || agent.MaxParallelTools > maxParallelTools {
		addIssue("agent.max_parallel_tools", fmt.Sprintf("must be greater than 0 and at most %d", maxParallelTools))
	}
}

func validateApproval(approval ApprovalConfig, addIssue func(string, string)) {
	switch approval.Default {
	case ApprovalAsk, ApprovalAllow, ApprovalDeny:
	default:
		addIssue("approval.default", fmt.Sprintf("must be %q, %q, or %q", ApprovalAsk, ApprovalAllow, ApprovalDeny))
	}
}

func validateLogging(logging LoggingConfig, addIssue func(string, string)) {
	switch logging.Level {
	case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
	default:
		addIssue("logging.level", fmt.Sprintf("must be %q, %q, %q, or %q", LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError))
	}
}
