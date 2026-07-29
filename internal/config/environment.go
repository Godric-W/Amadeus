package config

import (
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

type EnvLookup func(string) (string, bool)

var environmentReference = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}`)

func expandEnvironment(node *yaml.Node, path []string, lookup EnvLookup) error {
	switch node.Kind {
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index]
			value := node.Content[index+1]
			if err := expandEnvironment(value, appendPath(path, key.Value), lookup); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for index, child := range node.Content {
			segment := fmt.Sprintf("[%d]", index)
			if err := expandEnvironment(child, appendPath(path, segment), lookup); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		expanded, err := expandScalar(node.Value, formatPath(path), lookup)
		if err != nil {
			return err
		}
		node.Value = expanded
	}

	return nil
}

func expandScalar(value, path string, lookup EnvLookup) (string, error) {
	references := environmentReference.FindAllString(value, -1)
	if len(references) == 0 {
		return value, nil
	}

	expanded := value
	resolved := make(map[string]string, len(references))
	for _, reference := range references {
		name := reference[2 : len(reference)-1]
		resolvedValue, ok := resolved[name]
		if !ok {
			var found bool
			resolvedValue, found = lookup(name)
			if !found {
				return "", fmt.Errorf("%s: environment variable %s is not set", path, name)
			}
			resolved[name] = resolvedValue
		}
		expanded = strings.ReplaceAll(expanded, reference, resolvedValue)
	}

	return expanded, nil
}

func appendPath(path []string, segment string) []string {
	appended := make([]string, len(path)+1)
	copy(appended, path)
	appended[len(path)] = segment
	return appended
}

func formatPath(path []string) string {
	if len(path) == 0 {
		return "config"
	}

	var builder strings.Builder
	for index, segment := range path {
		if strings.HasPrefix(segment, "[") {
			builder.WriteString(segment)
			continue
		}
		if index > 0 {
			builder.WriteByte('.')
		}
		builder.WriteString(segment)
	}

	return builder.String()
}
