package config

import (
	"fmt"
	"strconv"

	"go.yaml.in/yaml/v3"
)

func migrateConfigDocument(root *yaml.Node) error {
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	if err := renameMappingKey(root, "default_provider", "model_provider"); err != nil {
		return err
	}
	if err := renameMappingKey(root, "providers", "model_providers"); err != nil {
		return err
	}

	providers := mappingValue(root, "model_providers")
	if providers != nil && providers.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(providers.Content); index += 2 {
			providerName := providers.Content[index].Value
			provider := providers.Content[index+1]
			if provider.Kind != yaml.MappingNode {
				continue
			}
			if err := renameMappingKey(provider, "api", "wire_api"); err != nil {
				return fmt.Errorf("model_providers.%s: %w", providerName, err)
			}
			if err := renameMappingKey(provider, "max_retries", "request_max_retries"); err != nil {
				return fmt.Errorf("model_providers.%s: %w", providerName, err)
			}
			for _, removed := range []string{"temperature", "max_output_tokens"} {
				if mappingValue(provider, removed) != nil {
					return fmt.Errorf("model_providers.%s.%s was removed in config version 2; Amadeus now uses the model provider default", providerName, removed)
				}
			}
		}
		for _, promotion := range []struct {
			legacy string
			current string
		}{
			{legacy: "model", current: "model"},
			{legacy: "context_window", current: "model_context_window"},
			{legacy: "auto_compact_token_limit", current: "model_auto_compact_token_limit"},
			{legacy: "tool_output_max_tokens", current: "tool_output_token_limit"},
		} {
			if err := promoteProviderField(root, providers, promotion.legacy, promotion.current); err != nil {
				return err
			}
		}
	}

	migrateVersion(root)
	return nil
}

func promoteProviderField(root, providers *yaml.Node, legacy, current string) error {
	type candidate struct {
		provider string
		value    *yaml.Node
	}
	candidates := make([]candidate, 0)
	for index := 0; index+1 < len(providers.Content); index += 2 {
		providerName := providers.Content[index].Value
		provider := providers.Content[index+1]
		if provider.Kind != yaml.MappingNode {
			continue
		}
		if value := mappingValue(provider, legacy); value != nil {
			candidates = append(candidates, candidate{provider: providerName, value: value})
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	if mappingValue(root, current) != nil {
		return fmt.Errorf("%s and legacy model_providers.*.%s cannot both be set", current, legacy)
	}
	first := candidates[0]
	if first.value.Kind != yaml.ScalarNode {
		return fmt.Errorf("model_providers.%s.%s must be a scalar to migrate to %s", first.provider, legacy, current)
	}
	for _, candidate := range candidates[1:] {
		if candidate.value.Kind != yaml.ScalarNode || candidate.value.Tag != first.value.Tag || candidate.value.Value != first.value.Value {
			return fmt.Errorf("cannot migrate model_providers.*.%s to %s because providers define different values", legacy, current)
		}
	}
	appendMappingValue(root, current, cloneScalarNode(first.value))
	for index := 0; index+1 < len(providers.Content); index += 2 {
		removeMappingKey(providers.Content[index+1], legacy)
	}
	return nil
}

func migrateVersion(root *yaml.Node) {
	version := mappingValue(root, "version")
	if version == nil {
		appendMappingValue(root, "version", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(CurrentVersion)})
		return
	}
	if version.Kind == yaml.ScalarNode && version.Value == "1" {
		version.Tag = "!!int"
		version.Value = strconv.Itoa(CurrentVersion)
	}
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

func renameMappingKey(node *yaml.Node, legacy, current string) error {
	legacyIndex := mappingKeyIndex(node, legacy)
	if legacyIndex < 0 {
		return nil
	}
	if mappingKeyIndex(node, current) >= 0 {
		return fmt.Errorf("%s and legacy %s cannot both be set", current, legacy)
	}
	node.Content[legacyIndex].Value = current
	return nil
}

func mappingKeyIndex(node *yaml.Node, key string) int {
	if node == nil || node.Kind != yaml.MappingNode {
		return -1
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return index
		}
	}
	return -1
}

func appendMappingValue(node *yaml.Node, key string, value *yaml.Node) {
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

func removeMappingKey(node *yaml.Node, key string) {
	index := mappingKeyIndex(node, key)
	if index < 0 {
		return
	}
	node.Content = append(node.Content[:index], node.Content[index+2:]...)
}

func cloneScalarNode(node *yaml.Node) *yaml.Node {
	cloned := *node
	cloned.Content = nil
	return &cloned
}
