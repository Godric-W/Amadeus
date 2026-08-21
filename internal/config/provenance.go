package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

type SourceKind string

const (
	SourceDefault     SourceKind = "default"
	SourceFile        SourceKind = "file"
	SourceEnvironment SourceKind = "environment"
	SourceCLI         SourceKind = "cli"
)

type Source struct {
	Kind   SourceKind
	Detail string
}

func (source Source) String() string {
	if source.Detail == "" {
		return string(source.Kind)
	}
	return fmt.Sprintf("%s: %s", source.Kind, source.Detail)
}

type Sources map[string]Source

func SourcesFor(configured Config) Sources {
	sources := make(Sources)
	set := func(path string) {
		sources[path] = Source{Kind: SourceDefault}
	}

	set("version")
	for _, path := range []string{"model", "model_provider", "model_context_window", "model_reasoning_effort", "model_input_modalities", "model_supports_original_image_detail", "model_auto_compact_token_limit", "tool_output_token_limit"} {
		set(path)
	}
	providerNames := make([]string, 0, len(configured.ModelProviders))
	for name := range configured.ModelProviders {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)
	for _, name := range providerNames {
		prefix := "model_providers." + name + "."
		for _, field := range []string{"wire_api", "dialect", "api_key", "base_url", "timeout", "request_max_retries", "stream_max_retries", "stream_idle_timeout"} {
			set(prefix + field)
		}
	}

	for _, path := range []string{
		"agent.max_parallel_tools",
		"agent.multi_agent.enabled",
		"agent.multi_agent.max_agents",
		"agent.multi_agent.max_depth",
		"agent.multi_agent.child_max_samples",
		"agent.multi_agent.child_max_tool_calls",
		"agent.multi_agent.child_max_duration",
		"web.fetch.enabled",
		"web.fetch.timeout",
		"web.fetch.max_bytes",
		"web.fetch.max_redirects",
		"web.search.enabled",
		"web.search.provider",
		"web.search.api_key",
		"web.search.base_url",
		"web.search.timeout",
		"web.search.max_results",
		"logging.level",
		"logging.trace_llm",
	} {
		set(path)
	}
	return sources
}

func InspectYAMLSources(path string) (Sources, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Sources{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open config source %q: %w", path, err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	var document yaml.Node
	if err := decoder.Decode(&document); errors.Is(err, io.EOF) {
		return Sources{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("decode config source %q: %w", path, err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("decode config source %q: %w", path, err)
		}
		return nil, errors.New("multiple YAML documents are not supported")
	}

	sources := make(Sources)
	if len(document.Content) > 0 {
		collectYAMLSources(document.Content[0], nil, path, sources)
	}
	return sources, nil
}

func EnvironmentOverrideSources(configured Config, lookup EnvLookup) Sources {
	sources := make(Sources)
	provider := configured.ModelProvider
	if value, ok := lookup(EnvModelProvider); ok {
		provider = value
		sources["model_provider"] = Source{Kind: SourceEnvironment, Detail: EnvModelProvider}
	}
	if _, ok := lookup(EnvModel); ok {
		sources["model"] = Source{Kind: SourceEnvironment, Detail: EnvModel}
	}
	if _, ok := lookup(EnvModelReasoningEffort); ok {
		sources["model_reasoning_effort"] = Source{Kind: SourceEnvironment, Detail: EnvModelReasoningEffort}
	}
	if _, ok := lookup(EnvModelInputModalities); ok {
		sources["model_input_modalities"] = Source{Kind: SourceEnvironment, Detail: EnvModelInputModalities}
	}
	if _, ok := lookup(EnvModelSupportsOriginalImageDetail); ok {
		sources["model_supports_original_image_detail"] = Source{Kind: SourceEnvironment, Detail: EnvModelSupportsOriginalImageDetail}
	}

	for variable, field := range map[string]string{
		EnvWireAPI: "wire_api",
		EnvDialect: "dialect",
		EnvAPIKey:  "api_key",
		EnvBaseURL: "base_url",
	} {
		if _, ok := lookup(variable); ok {
			sources["model_providers."+provider+"."+field] = Source{Kind: SourceEnvironment, Detail: variable}
		}
	}

	return sources
}

func MergeSources(base Sources, overlays ...Sources) Sources {
	merged := make(Sources, len(base))
	for path, source := range base {
		merged[path] = source
	}
	for _, overlay := range overlays {
		for path, source := range overlay {
			merged[path] = source
		}
	}
	return merged
}

func collectYAMLSources(node *yaml.Node, path []string, filePath string, sources Sources) {
	switch node.Kind {
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index]
			value := node.Content[index+1]
			collectYAMLSources(value, appendPath(path, key.Value), filePath, sources)
		}
	case yaml.SequenceNode:
		for index, child := range node.Content {
			collectYAMLSources(child, appendPath(path, fmt.Sprintf("[%d]", index)), filePath, sources)
		}
	case yaml.ScalarNode:
		fieldPath := formatPath(path)
		references := environmentReference.FindAllString(node.Value, -1)
		if len(references) > 0 {
			names := make([]string, 0, len(references))
			seen := make(map[string]struct{}, len(references))
			for _, reference := range references {
				name := reference[2 : len(reference)-1]
				if _, ok := seen[name]; ok {
					continue
				}
				seen[name] = struct{}{}
				names = append(names, name)
			}
			sources[fieldPath] = Source{Kind: SourceEnvironment, Detail: strings.Join(names, ", ") + " via " + filePath}
			return
		}
		sources[fieldPath] = Source{Kind: SourceFile, Detail: filePath}
	}
}
