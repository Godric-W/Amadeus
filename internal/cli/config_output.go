package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func writeConfigExplanation(writer io.Writer, path string, configured config.Config, sources config.Sources) {
	fmt.Fprintf(writer, "config: %s\n", path)
	writeExplainedValue(writer, "model", configured.Model, sources)
	writeExplainedValue(writer, "model_provider", configured.ModelProvider, sources)
	writeExplainedValue(writer, "model_context_window", configured.ModelContextWindow, sources)
	writeExplainedValue(writer, "model_reasoning_effort", displayReasoningEffort(configured.ModelReasoningEffort), sources)
	writeExplainedValue(writer, "model_input_modalities", configured.ModelInputModalities, sources)
	writeExplainedValue(writer, "model_supports_original_image_detail", configured.ModelSupportsOriginalImageDetail, sources)
	writeExplainedValue(writer, "model_auto_compact_token_limit", configured.ModelAutoCompactTokenLimit, sources)
	writeExplainedValue(writer, "tool_output_token_limit", configured.ToolOutputTokenLimit, sources)

	providerNames := make([]string, 0, len(configured.ModelProviders))
	for name := range configured.ModelProviders {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)
	for _, name := range providerNames {
		provider := configured.ModelProviders[name]
		prefix := "model_providers." + name + "."
		writeExplainedValue(writer, prefix+"wire_api", provider.WireAPI, sources)
		writeExplainedValue(writer, prefix+"dialect", provider.Dialect, sources)
		writeExplainedValue(writer, prefix+"api_key", provider.APIKey, sources)
		writeExplainedValue(writer, prefix+"base_url", provider.BaseURL, sources)
		writeExplainedValue(writer, prefix+"timeout", provider.Timeout, sources)
		writeExplainedValue(writer, prefix+"request_max_retries", provider.RequestMaxRetries, sources)
		writeExplainedValue(writer, prefix+"stream_max_retries", provider.StreamMaxRetries, sources)
		writeExplainedValue(writer, prefix+"stream_idle_timeout", provider.StreamIdleTimeout, sources)
	}

	writeExplainedValue(writer, "agent.max_parallel_tools", configured.Agent.MaxParallelTools, sources)
	writeExplainedValue(writer, "web.fetch.enabled", configured.Web.Fetch.Enabled, sources)
	writeExplainedValue(writer, "web.fetch.timeout", configured.Web.Fetch.Timeout, sources)
	writeExplainedValue(writer, "web.fetch.max_bytes", configured.Web.Fetch.MaxBytes, sources)
	writeExplainedValue(writer, "web.fetch.max_redirects", configured.Web.Fetch.MaxRedirects, sources)
	writeExplainedValue(writer, "web.search.enabled", configured.Web.Search.Enabled, sources)
	writeExplainedValue(writer, "web.search.provider", configured.Web.Search.Provider, sources)
	writeExplainedValue(writer, "web.search.api_key", configured.Web.Search.APIKey, sources)
	writeExplainedValue(writer, "web.search.base_url", configured.Web.Search.BaseURL, sources)
	writeExplainedValue(writer, "web.search.timeout", configured.Web.Search.Timeout, sources)
	writeExplainedValue(writer, "web.search.max_results", configured.Web.Search.MaxResults, sources)
	writeExplainedValue(writer, "logging.level", configured.Logging.Level, sources)
	writeExplainedValue(writer, "logging.trace_llm", configured.Logging.TraceLLM, sources)
}

func displayReasoningEffort(effort *llm.ReasoningEffort) string {
	if effort == nil {
		return "provider default (unset)"
	}
	return string(*effort)
}

func writeExplainedValue(writer io.Writer, path string, value any, sources config.Sources) {
	source, ok := sources[path]
	if !ok {
		source = config.Source{Kind: config.SourceDefault}
	}
	fmt.Fprintf(writer, "%s: %v [source: %s]\n", path, value, source)
}
