package openai

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	openaisdk "github.com/openai/openai-go/v3"
)

type Client struct {
	sdk          openaisdk.Client
	providerName string
	provider     config.ProviderConfig
}

func NewAdapter(providerName string, provider config.ProviderConfig) (*Client, error) {
	if strings.TrimSpace(providerName) == "" {
		return nil, errors.New("provider name is empty")
	}
	if strings.TrimSpace(provider.Model) == "" {
		return nil, errors.New("provider model is empty")
	}
	switch provider.API {
	case config.APIResponses, config.APIChatCompletions:
	default:
		return nil, errors.New("provider API mode is unsupported")
	}

	sdk, err := NewClient(provider)
	if err != nil {
		return nil, err
	}
	return &Client{sdk: sdk, providerName: providerName, provider: provider}, nil
}

func (client *Client) Complete(ctx context.Context, request llm.Request) (llm.Response, error) {
	stream, err := client.Stream(ctx, request)
	if err != nil {
		return llm.Response{}, err
	}
	response, receiveErr := collectStream(stream)
	closeErr := stream.Close()
	if receiveErr != nil || closeErr != nil {
		return response, errors.Join(receiveErr, closeErr)
	}
	return response, nil
}

func (client *Client) Stream(ctx context.Context, request llm.Request) (llm.Stream, error) {
	switch client.provider.API {
	case config.APIResponses:
		return openResponsesStream(ctx, client.sdk, request)
	case config.APIChatCompletions:
		return openChatCompletionsStream(ctx, client.sdk, request)
	default:
		return nil, &llm.ProviderError{
			Kind:    llm.ProviderErrorInvalidRequest,
			Message: "provider API mode is unsupported",
		}
	}
}

func (client *Client) Model() llm.ModelInfo {
	return llm.ModelInfo{
		Provider: client.providerName,
		Name:     client.provider.Model,
	}
}

func (client *Client) Capabilities() llm.Capabilities {
	capabilities := llm.Capabilities{
		SupportsStreaming: true,
	}
	if client.provider.API == config.APIResponses {
		capabilities.SupportsDeveloperRole = true
		capabilities.SupportsReasoning = true
		capabilities.SupportsStreamUsage = true
		capabilities.SupportsPromptCacheUsage = true
	}
	return capabilities
}

func collectStream(stream llm.Stream) (llm.Response, error) {
	response := llm.Response{Message: llm.AssistantMessage("")}
	for {
		chunk, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return response, &llm.ProviderError{
					Kind:    llm.ProviderErrorProtocol,
					Message: "provider stream ended before completion",
				}
			}
			return response, err
		}
		if chunk.ID != "" {
			response.ID = chunk.ID
		}
		if chunk.RequestID != "" {
			response.RequestID = chunk.RequestID
		}
		response.Message.Content += chunk.ContentDelta
		response.Message.Reasoning += chunk.ReasoningDelta
		if chunk.Usage != nil {
			response.Usage = *chunk.Usage
		}
		if chunk.Completed() {
			response.FinishReason = chunk.FinishReason
			response.ProviderFinishReason = chunk.ProviderFinishReason
			return response, nil
		}
	}
}

var _ llm.Client = (*Client)(nil)
