package config

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

func migrateConfigDocument(root *yaml.Node) error {
	providers := mappingValue(root, "providers")
	if providers == nil || providers.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(providers.Content); index += 2 {
		providerName := providers.Content[index].Value
		provider := providers.Content[index+1]
		if provider.Kind != yaml.MappingNode {
			continue
		}
		if err := renameMappingKey(provider, "max_retries", "request_max_retries"); err != nil {
			return fmt.Errorf("providers.%s: %w", providerName, err)
		}
	}
	return nil
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
	legacyIndex := -1
	currentIndex := -1
	for index := 0; index+1 < len(node.Content); index += 2 {
		switch node.Content[index].Value {
		case legacy:
			legacyIndex = index
		case current:
			currentIndex = index
		}
	}
	if legacyIndex < 0 {
		return nil
	}
	if currentIndex >= 0 {
		return fmt.Errorf("%s and legacy %s cannot both be set", current, legacy)
	}
	node.Content[legacyIndex].Value = current
	return nil
}
