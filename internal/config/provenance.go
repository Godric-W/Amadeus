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
	set("default_provider")
	providerNames := make([]string, 0, len(configured.Providers))
	for name := range configured.Providers {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)
	for _, name := range providerNames {
		prefix := "providers." + name + "."
		for _, field := range []string{"api", "api_key", "base_url", "model", "timeout", "max_retries", "temperature", "max_output_tokens"} {
			set(prefix + field)
		}
	}

	for _, path := range []string{
		"agent.mode",
		"agent.max_steps",
		"agent.max_parallel_tools",
		"approval.enabled",
		"approval.default",
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
	provider := configured.DefaultProvider
	if value, ok := lookup(EnvProvider); ok {
		provider = value
		sources["default_provider"] = Source{Kind: SourceEnvironment, Detail: EnvProvider}
	}

	for variable, field := range map[string]string{
		EnvAPI:     "api",
		EnvAPIKey:  "api_key",
		EnvBaseURL: "base_url",
		EnvModel:   "model",
	} {
		if _, ok := lookup(variable); ok {
			sources["providers."+provider+"."+field] = Source{Kind: SourceEnvironment, Detail: variable}
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
