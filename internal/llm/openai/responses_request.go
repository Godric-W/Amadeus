package openai

import (
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
		role, err := responsesRole(message.Role)
		if err != nil {
			return responses.ResponseNewParams{}, fmt.Errorf("responses request messages[%d]: %w", index, err)
		}
		input = append(input, responses.ResponseInputItemParamOfMessage(message.Content, role))
	}

	return responses.ResponseNewParams{
		Model: shared.ResponsesModel(request.Model),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: input,
		},
		Temperature:     openaisdk.Float(request.Temperature),
		MaxOutputTokens: openaisdk.Int(int64(request.MaxOutputTokens)),
	}, nil
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
	case llm.RoleTool:
		return "", errors.New("tool messages are not supported before M2")
	default:
		return "", fmt.Errorf("unsupported role %q", role)
	}
}
