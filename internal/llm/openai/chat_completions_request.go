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
	inputMessages := request.InputMessages()
	if len(inputMessages) == 0 {
		return openaisdk.ChatCompletionNewParams{}, errors.New("chat completions request messages are empty")
	}
	messages := make([]openaisdk.ChatCompletionMessageParamUnion, 0, len(inputMessages))
	for index, message := range inputMessages {
		prepared := message
		if prepared.Role == llm.RoleDeveloper && !dialect.Capabilities(config.WireAPIChatCompletions).SupportsDeveloperRole {
			prepared.Role = llm.RoleSystem
		}
		converted, err := chatCompletionMessages(prepared)
		if err != nil {
			return openaisdk.ChatCompletionNewParams{}, fmt.Errorf("chat completions request messages[%d]: %w", index, err)
		}
		for convertedIndex := range converted {
			if err := dialect.PrepareChatMessage(prepared, &converted[convertedIndex]); err != nil {
				return openaisdk.ChatCompletionNewParams{}, fmt.Errorf("chat completions request messages[%d]: %w", index, err)
			}
		}
		messages = append(messages, converted...)
	}
	tools, err := chatCompletionTools(request.ToolSpecs(), dialect.SupportsStrictToolSchema())
	if err != nil {
		return openaisdk.ChatCompletionNewParams{}, err
	}

	params := openaisdk.ChatCompletionNewParams{
		Model:    shared.ChatModel(request.Model),
		Messages: messages,
		Tools:    tools,
	}
	if err := dialect.PrepareChatRequest(request, &params); err != nil {
		return openaisdk.ChatCompletionNewParams{}, err
	}
	return params, nil
}

func chatCompletionMessages(message llm.ResponseItem) ([]openaisdk.ChatCompletionMessageParamUnion, error) {
	switch message.Role {
	case llm.RoleSystem:
		if len(message.Parts) != 0 {
			return nil, errors.New("system message cannot contain image parts")
		}
		return []openaisdk.ChatCompletionMessageParamUnion{openaisdk.SystemMessage(message.Content)}, nil
	case llm.RoleDeveloper:
		if len(message.Parts) != 0 {
			return nil, errors.New("developer message cannot contain image parts")
		}
		return []openaisdk.ChatCompletionMessageParamUnion{openaisdk.DeveloperMessage(message.Content)}, nil
	case llm.RoleUser:
		parts, err := chatContentParts(message)
		if err != nil {
			return nil, err
		}
		if parts == nil {
			return []openaisdk.ChatCompletionMessageParamUnion{openaisdk.UserMessage(message.Content)}, nil
		}
		return []openaisdk.ChatCompletionMessageParamUnion{openaisdk.UserMessage(parts)}, nil
	case llm.RoleAssistant:
		if len(message.Parts) != 0 {
			return nil, errors.New("assistant message cannot contain image parts")
		}
		assistant := openaisdk.AssistantMessage(message.Content)
		for index, call := range message.ToolCalls {
			if err := validateToolCall(call); err != nil {
				return nil, fmt.Errorf("tool_calls[%d]: %w", index, err)
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
		return []openaisdk.ChatCompletionMessageParamUnion{assistant}, nil
	case llm.RoleTool:
		if strings.TrimSpace(message.ToolCallID) == "" {
			return nil, errors.New("tool result call ID is empty")
		}
		if len(message.ToolCalls) != 0 {
			return nil, errors.New("tool result cannot contain tool calls")
		}
		messages := []openaisdk.ChatCompletionMessageParamUnion{openaisdk.ToolMessage(message.Content, message.ToolCallID)}
		if len(message.Parts) != 0 {
			parts, err := chatContentParts(llm.ResponseItem{Content: "Image output from tool call " + message.ToolCallID + ".", Parts: message.Parts})
			if err != nil {
				return nil, err
			}
			messages = append(messages, openaisdk.UserMessage(parts))
		}
		return messages, nil
	default:
		return nil, fmt.Errorf("unsupported role %q", message.Role)
	}
}

func chatContentParts(message llm.ResponseItem) ([]openaisdk.ChatCompletionContentPartUnionParam, error) {
	if len(message.Parts) == 0 {
		return nil, nil
	}
	parts := make([]openaisdk.ChatCompletionContentPartUnionParam, 0, len(message.Parts)+1)
	if message.Content != "" {
		parts = append(parts, openaisdk.TextContentPart(message.Content))
	}
	for _, part := range message.Parts {
		switch part.Kind {
		case llm.ContentText:
			parts = append(parts, openaisdk.TextContentPart(part.Text))
		case llm.ContentImage:
			url, err := imageDataURL(part)
			if err != nil {
				return nil, err
			}
			parts = append(parts, openaisdk.ImageContentPart(openaisdk.ChatCompletionContentPartImageImageURLParam{URL: url, Detail: chatImageDetail(part.Detail)}))
		default:
			return nil, fmt.Errorf("unsupported content part kind %q", part.Kind)
		}
	}
	return parts, nil
}

func chatImageDetail(detail string) string {
	switch strings.TrimSpace(detail) {
	case "high", "original":
		return "high"
	default:
		return "auto"
	}
}

func chatCompletionTools(definitions []llm.ToolSpec, supportsStrict bool) ([]openaisdk.ChatCompletionToolUnionParam, error) {
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
