package openai

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

func newChatCompletionsRequest(request llm.Request) (openaisdk.ChatCompletionNewParams, error) {
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
		messages = append(messages, converted)
	}

	return openaisdk.ChatCompletionNewParams{
		Model:       shared.ChatModel(request.Model),
		Messages:    messages,
		Temperature: openaisdk.Float(request.Temperature),
		MaxTokens:   openaisdk.Int(int64(request.MaxOutputTokens)),
	}, nil
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
		return openaisdk.AssistantMessage(message.Content), nil
	case llm.RoleTool:
		return openaisdk.ChatCompletionMessageParamUnion{}, errors.New("tool messages are not supported before M2")
	default:
		return openaisdk.ChatCompletionMessageParamUnion{}, fmt.Errorf("unsupported role %q", message.Role)
	}
}
