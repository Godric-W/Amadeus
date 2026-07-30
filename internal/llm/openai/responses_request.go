package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

func newResponsesRequest(request llm.Request) (responses.ResponseNewParams, error) {
	if strings.TrimSpace(request.Model) == "" {
		return responses.ResponseNewParams{}, errors.New("responses request model is empty")
	}
	if len(request.Messages) == 0 {
		return responses.ResponseNewParams{}, errors.New("responses request messages are empty")
	}
	if request.Temperature < 0 || request.Temperature > 2 {
		return responses.ResponseNewParams{}, errors.New("responses request temperature must be between 0 and 2")
	}
	if request.MaxOutputTokens <= 0 {
		return responses.ResponseNewParams{}, errors.New("responses request max output tokens must be greater than zero")
	}

	input := make(responses.ResponseInputParam, 0, len(request.Messages))
	for index, message := range request.Messages {
		converted, err := responsesInputItems(message)
		if err != nil {
			return responses.ResponseNewParams{}, fmt.Errorf("responses request messages[%d]: %w", index, err)
		}
		input = append(input, converted...)
	}

	tools, err := responsesTools(request.Tools)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}

	return responses.ResponseNewParams{
		Model: shared.ResponsesModel(request.Model),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: input,
		},
		Temperature:     openaisdk.Float(request.Temperature),
		MaxOutputTokens: openaisdk.Int(int64(request.MaxOutputTokens)),
		Tools:           tools,
	}, nil
}

func responsesInputItems(message llm.Message) ([]responses.ResponseInputItemUnionParam, error) {
	if message.Role == llm.RoleTool {
		if strings.TrimSpace(message.ToolCallID) == "" {
			return nil, errors.New("tool result call ID is empty")
		}
		if len(message.ToolCalls) != 0 {
			return nil, errors.New("tool result cannot contain tool calls")
		}
		return []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfFunctionCallOutput(message.ToolCallID, message.Content),
		}, nil
	}

	role, err := responsesRole(message.Role)
	if err != nil {
		return nil, err
	}
	if message.Role != llm.RoleAssistant && len(message.ToolCalls) != 0 {
		return nil, errors.New("only assistant messages can contain tool calls")
	}

	items := make([]responses.ResponseInputItemUnionParam, 0, 1+len(message.ToolCalls))
	if message.Content != "" || len(message.ToolCalls) == 0 {
		items = append(items, responses.ResponseInputItemParamOfMessage(message.Content, role))
	}
	for index, call := range message.ToolCalls {
		if err := validateToolCall(call); err != nil {
			return nil, fmt.Errorf("tool_calls[%d]: %w", index, err)
		}
		items = append(items, responses.ResponseInputItemParamOfFunctionCall(string(call.Arguments), call.ID, call.Name))
	}
	return items, nil
}

func responsesTools(definitions []llm.ToolDefinition) ([]responses.ToolUnionParam, error) {
	tools := make([]responses.ToolUnionParam, 0, len(definitions))
	seen := make(map[string]struct{}, len(definitions))
	for index, definition := range definitions {
		schema, err := toolSchema(definition, seen)
		if err != nil {
			return nil, fmt.Errorf("responses request tools[%d]: %w", index, err)
		}
		tool := responses.ToolParamOfFunction(definition.Name, schema, definition.Strict)
		if definition.Description != "" {
			tool.OfFunction.Description = openaisdk.String(definition.Description)
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func toolSchema(definition llm.ToolDefinition, seen map[string]struct{}) (map[string]any, error) {
	if strings.TrimSpace(definition.Name) == "" {
		return nil, errors.New("name is empty")
	}
	if _, ok := seen[definition.Name]; ok {
		return nil, fmt.Errorf("duplicate name %q", definition.Name)
	}
	seen[definition.Name] = struct{}{}
	var schema map[string]any
	if len(definition.InputSchema) == 0 {
		return nil, errors.New("input schema is empty")
	}
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
		return nil, fmt.Errorf("input schema is invalid JSON: %w", err)
	}
	if schema == nil {
		return nil, errors.New("input schema must be a JSON object")
	}
	return schema, nil
}

func validateToolCall(call llm.ToolCall) error {
	if strings.TrimSpace(call.ID) == "" {
		return errors.New("call ID is empty")
	}
	if strings.TrimSpace(call.Name) == "" {
		return errors.New("name is empty")
	}
	if len(call.Arguments) == 0 || !json.Valid(call.Arguments) {
		return errors.New("arguments must be valid JSON")
	}
	return nil
}

func responsesRole(role llm.Role) (responses.EasyInputMessageRole, error) {
	switch role {
	case llm.RoleSystem:
		return responses.EasyInputMessageRoleSystem, nil
	case llm.RoleDeveloper:
		return responses.EasyInputMessageRoleDeveloper, nil
	case llm.RoleUser:
		return responses.EasyInputMessageRoleUser, nil
	case llm.RoleAssistant:
		return responses.EasyInputMessageRoleAssistant, nil
	default:
		return "", fmt.Errorf("unsupported role %q", role)
	}
}
