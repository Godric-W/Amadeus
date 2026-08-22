package openai

import (
	"encoding/base64"
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
	messages := request.InputMessages()
	if len(messages) == 0 {
		return responses.ResponseNewParams{}, errors.New("responses request messages are empty")
	}
	input := make(responses.ResponseInputParam, 0, len(messages))
	for index, message := range messages {
		converted, err := responsesInputItems(message)
		if err != nil {
			return responses.ResponseNewParams{}, fmt.Errorf("responses request messages[%d]: %w", index, err)
		}
		input = append(input, converted...)
	}

	tools, err := responsesTools(request.ToolSpecs())
	if err != nil {
		return responses.ResponseNewParams{}, err
	}

	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(request.Model),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: input,
		},
		Tools:    tools,
		Metadata: shared.Metadata(request.Metadata.Values()),
	}
	effort, configured, err := reasoningEffort(request)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	if configured {
		params.Reasoning = shared.ReasoningParam{Effort: shared.ReasoningEffort(effort)}
	}
	return params, nil
}

func responsesInputItems(message llm.ResponseItem) ([]responses.ResponseInputItemUnionParam, error) {
	if message.Role == llm.RoleTool {
		if strings.TrimSpace(message.ToolCallID) == "" {
			return nil, errors.New("tool result call ID is empty")
		}
		if len(message.ToolCalls) != 0 {
			return nil, errors.New("tool result cannot contain tool calls")
		}
		if len(message.Parts) == 0 {
			return []responses.ResponseInputItemUnionParam{responses.ResponseInputItemParamOfFunctionCallOutput(message.ToolCallID, message.Content)}, nil
		}
		output, err := responsesToolOutput(message)
		if err != nil {
			return nil, err
		}
		return []responses.ResponseInputItemUnionParam{responses.ResponseInputItemParamOfFunctionCallOutput(message.ToolCallID, output)}, nil
	}

	role, err := responsesRole(message.Role)
	if err != nil {
		return nil, err
	}
	if message.Role != llm.RoleAssistant && len(message.ToolCalls) != 0 {
		return nil, errors.New("only assistant messages can contain tool calls")
	}

	items := make([]responses.ResponseInputItemUnionParam, 0, 1+len(message.ToolCalls))
	if message.Content != "" || len(message.Parts) != 0 || len(message.ToolCalls) == 0 {
		content, err := responsesMessageContent(message)
		if err != nil {
			return nil, err
		}
		if content == nil {
			items = append(items, responses.ResponseInputItemParamOfMessage(message.Content, role))
		} else {
			items = append(items, responses.ResponseInputItemParamOfMessage(content, role))
		}
	}
	for index, call := range message.ToolCalls {
		if err := validateToolCall(call); err != nil {
			return nil, fmt.Errorf("tool_calls[%d]: %w", index, err)
		}
		items = append(items, responses.ResponseInputItemParamOfFunctionCall(string(call.Arguments), call.ID, call.Name))
	}
	return items, nil
}

func responsesMessageContent(message llm.ResponseItem) (responses.ResponseInputMessageContentListParam, error) {
	if len(message.Parts) == 0 {
		return nil, nil
	}
	parts := make(responses.ResponseInputMessageContentListParam, 0, len(message.Parts)+1)
	if message.Content != "" {
		parts = append(parts, responses.ResponseInputContentParamOfInputText(message.Content))
	}
	for _, part := range message.Parts {
		converted, err := responsesContentPart(part)
		if err != nil {
			return nil, err
		}
		parts = append(parts, converted)
	}
	return parts, nil
}

func responsesToolOutput(message llm.ResponseItem) (responses.ResponseFunctionCallOutputItemListParam, error) {
	parts := make(responses.ResponseFunctionCallOutputItemListParam, 0, len(message.Parts)+1)
	if message.Content != "" {
		parts = append(parts, responses.ResponseFunctionCallOutputItemParamOfInputText(message.Content))
	}
	for _, part := range message.Parts {
		switch part.Kind {
		case llm.ContentText:
			parts = append(parts, responses.ResponseFunctionCallOutputItemParamOfInputText(part.Text))
		case llm.ContentImage:
			url, err := imageDataURL(part)
			if err != nil {
				return nil, err
			}
			parts = append(parts, responses.ResponseFunctionCallOutputItemUnionParam{OfInputImage: &responses.ResponseInputImageContentParam{ImageURL: openaisdk.String(url), Detail: responses.ResponseInputImageContentDetail(responsesImageDetail(part.Detail))}})
		default:
			return nil, fmt.Errorf("unsupported content part kind %q", part.Kind)
		}
	}
	return parts, nil
}

func responsesContentPart(part llm.ContentPart) (responses.ResponseInputContentUnionParam, error) {
	switch part.Kind {
	case llm.ContentText:
		return responses.ResponseInputContentParamOfInputText(part.Text), nil
	case llm.ContentImage:
		url, err := imageDataURL(part)
		if err != nil {
			return responses.ResponseInputContentUnionParam{}, err
		}
		value := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetail(responsesImageDetail(part.Detail)))
		value.OfInputImage.ImageURL = openaisdk.String(url)
		return value, nil
	default:
		return responses.ResponseInputContentUnionParam{}, fmt.Errorf("unsupported content part kind %q", part.Kind)
	}
}

func responsesImageDetail(detail string) string {
	switch strings.TrimSpace(detail) {
	case "high", "original":
		return strings.TrimSpace(detail)
	default:
		return "auto"
	}
}

func imageDataURL(part llm.ContentPart) (string, error) {
	if !strings.HasPrefix(part.MediaType, "image/") || strings.TrimSpace(part.Data) == "" {
		return "", errors.New("image content part requires media type and base64 data")
	}
	if _, err := base64.StdEncoding.DecodeString(part.Data); err != nil {
		return "", fmt.Errorf("image content part data is invalid base64: %w", err)
	}
	return "data:" + part.MediaType + ";base64," + part.Data, nil
}

func responsesTools(definitions []llm.ToolSpec) ([]responses.ToolUnionParam, error) {
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

func toolSchema(definition llm.ToolSpec, seen map[string]struct{}) (map[string]any, error) {
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
