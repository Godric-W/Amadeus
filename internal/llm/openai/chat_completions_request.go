package openai

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

func newChatCompletionsRequest(request llm.Request) (openaisdk.ChatCompletionNewParams, error) {
	standard, err := resolveDialect(config.DialectStandard)
	if err != nil {
		return openaisdk.ChatCompletionNewParams{}, err
	}
	return newChatCompletionsRequestForDialect(request, standard)
}

func newChatCompletionsRequestForDialect(request llm.Request, dialect Dialect) (openaisdk.ChatCompletionNewParams, error) {
	if strings.TrimSpace(request.Model) == "" {
		return openaisdk.ChatCompletionNewParams{}, errors.New("chat completions request model is empty")
	}
	if len(request.Messages) == 0 {
		return openaisdk.ChatCompletionNewParams{}, errors.New("chat completions request messages are empty")
	}
	if request.Temperature < 0 || request.Temperature > 2 {
		return openaisdk.ChatCompletionNewParams{}, errors.New("chat completions request temperature must be between 0 and 2")
	}
	if request.MaxOutputTokens <= 0 {
		return openaisdk.ChatCompletionNewParams{}, errors.New("chat completions request max output tokens must be greater than zero")
	}

	messages := make([]openaisdk.ChatCompletionMessageParamUnion, 0, len(request.Messages))
	for index, message := range request.Messages {
		converted, err := chatCompletionMessage(message)
		if err != nil {
			return openaisdk.ChatCompletionNewParams{}, fmt.Errorf("chat completions request messages[%d]: %w", index, err)
		}
		if err := dialect.PrepareChatMessage(message, &converted); err != nil {
			return openaisdk.ChatCompletionNewParams{}, fmt.Errorf("chat completions request messages[%d]: %w", index, err)
		}
		messages = append(messages, converted)
	}
	tools, err := chatCompletionTools(request.Tools, dialect.SupportsStrictToolSchema())
	if err != nil {
		return openaisdk.ChatCompletionNewParams{}, err
	}

	params := openaisdk.ChatCompletionNewParams{
		Model:       shared.ChatModel(request.Model),
		Messages:    messages,
		Temperature: openaisdk.Float(request.Temperature),
		MaxTokens:   openaisdk.Int(int64(request.MaxOutputTokens)),
		Tools:       tools,
	}
	if err := dialect.PrepareChatRequest(request, &params); err != nil {
		return openaisdk.ChatCompletionNewParams{}, err
	}
	return params, nil
}

func chatCompletionMessage(message llm.Message) (openaisdk.ChatCompletionMessageParamUnion, error) {
	switch message.Role {
	case llm.RoleSystem:
		return openaisdk.SystemMessage(message.Content), nil
	case llm.RoleDeveloper:
		return openaisdk.DeveloperMessage(message.Content), nil
	case llm.RoleUser:
		return openaisdk.UserMessage(message.Content), nil
	case llm.RoleAssistant:
		assistant := openaisdk.AssistantMessage(message.Content)
		for index, call := range message.ToolCalls {
			if err := validateToolCall(call); err != nil {
				return openaisdk.ChatCompletionMessageParamUnion{}, fmt.Errorf("tool_calls[%d]: %w", index, err)
			}
			assistant.OfAssistant.ToolCalls = append(assistant.OfAssistant.ToolCalls, openaisdk.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openaisdk.ChatCompletionMessageFunctionToolCallParam{
					ID: call.ID,
					Function: openaisdk.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name: call.Name, Arguments: string(call.Arguments),
					},
				},
			})
		}
		return assistant, nil
	case llm.RoleTool:
		if strings.TrimSpace(message.ToolCallID) == "" {
			return openaisdk.ChatCompletionMessageParamUnion{}, errors.New("tool result call ID is empty")
		}
		if len(message.ToolCalls) != 0 {
			return openaisdk.ChatCompletionMessageParamUnion{}, errors.New("tool result cannot contain tool calls")
		}
		return openaisdk.ToolMessage(message.Content, message.ToolCallID), nil
	default:
		return openaisdk.ChatCompletionMessageParamUnion{}, fmt.Errorf("unsupported role %q", message.Role)
	}
}

func chatCompletionTools(definitions []llm.ToolDefinition, supportsStrict bool) ([]openaisdk.ChatCompletionToolUnionParam, error) {
	tools := make([]openaisdk.ChatCompletionToolUnionParam, 0, len(definitions))
	seen := make(map[string]struct{}, len(definitions))
	for index, definition := range definitions {
		schema, err := toolSchema(definition, seen)
		if err != nil {
			return nil, fmt.Errorf("chat completions request tools[%d]: %w", index, err)
		}
		function := shared.FunctionDefinitionParam{
			Name:       definition.Name,
			Parameters: schema,
		}
		if supportsStrict {
			function.Strict = openaisdk.Bool(definition.Strict)
		}
		if definition.Description != "" {
			function.Description = openaisdk.String(definition.Description)
		}
		tools = append(tools, openaisdk.ChatCompletionFunctionTool(function))
	}
	return tools, nil
}
