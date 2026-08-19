package mcp

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func sanitizeSchema(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{"type":"object","additionalProperties":true}`), nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("schema root is not an object")
	}
	cleanSchema(object)
	if _, ok := object["type"]; !ok {
		object["type"] = "object"
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func validateToolArguments(schema json.RawMessage, arguments json.RawMessage) error {
	if len(schema) == 0 {
		return nil
	}
	compiler := jsonschema.NewCompiler()
	var schemaValue any
	if err := json.Unmarshal(schema, &schemaValue); err != nil {
		return fmt.Errorf("decode MCP tool input schema: %w", err)
	}
	if err := compiler.AddResource("mcp://tool-input-schema", schemaValue); err != nil {
		return fmt.Errorf("compile MCP tool input schema: %w", err)
	}
	compiled, err := compiler.Compile("mcp://tool-input-schema")
	if err != nil {
		return fmt.Errorf("compile MCP tool input schema: %w", err)
	}
	var value any
	if len(arguments) == 0 || string(arguments) == "null" {
		value = map[string]any{}
	} else if err := json.Unmarshal(arguments, &value); err != nil {
		return fmt.Errorf("decode MCP tool arguments: %w", err)
	}
	if err := compiled.Validate(value); err != nil {
		return fmt.Errorf("MCP tool arguments do not match input schema: %w", err)
	}
	return nil
}

func cleanSchema(value any) {
	switch current := value.(type) {
	case map[string]any:
		for _, key := range []string{"$schema", "$id", "$defs", "definitions", "examples", "default", "title", "format", "contentEncoding", "contentMediaType"} {
			delete(current, key)
		}
		for _, child := range current {
			cleanSchema(child)
		}
	case []any:
		for _, child := range current {
			cleanSchema(child)
		}
	}
}
