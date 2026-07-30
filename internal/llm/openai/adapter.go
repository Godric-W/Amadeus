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

type Adapter struct {
	sdk          openaisdk.Client
	providerName string
	provider     config.ProviderConfig
	dialect      Dialect
}

func NewAdapter(providerName string, provider config.ProviderConfig) (*Adapter, error) {
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
	dialect, err := resolveDialect(provider.Dialect)
	if err != nil {
		return nil, err
	}
	if !dialect.SupportsAPI(provider.API) {
		return nil, &DialectError{
			Dialect: provider.Dialect,
			API:     provider.API,
			Reason:  "select chat_completions or choose a compatible dialect",
		}
	}

	sdk, err := newSDKClient(provider, nil)
	if err != nil {
		return nil, err
	}
	return &Adapter{sdk: sdk, providerName: providerName, provider: provider, dialect: dialect}, nil
}

func (adapter *Adapter) Complete(ctx context.Context, request llm.Request) (llm.Response, error) {
	stream, err := adapter.Stream(ctx, request)
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

func (adapter *Adapter) Stream(ctx context.Context, request llm.Request) (llm.Stream, error) {
	switch adapter.provider.API {
	case config.APIResponses:
		return openResponsesStream(ctx, adapter.sdk, request)
	case config.APIChatCompletions:
		return openChatCompletionsStreamForDialect(ctx, adapter.sdk, request, adapter.dialect)
	default:
		return nil, &llm.ProviderError{
			Kind:    llm.ProviderErrorInvalidRequest,
			Message: "provider API mode is unsupported",
		}
	}
}

func (adapter *Adapter) Model() llm.ModelInfo {
	return llm.ModelInfo{
		Provider: adapter.providerName,
		Name:     adapter.provider.Model,
	}
}

func (adapter *Adapter) Capabilities() llm.Capabilities {
	return adapter.dialect.Capabilities(adapter.provider.API)
}

func (adapter *Adapter) Dialect() config.ProviderDialect {
	return adapter.dialect.Name()
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
		if len(chunk.ToolCalls) != 0 {
			response.Message.ToolCalls = append(response.Message.ToolCalls, chunk.ToolCalls...)
		}
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

var _ llm.Client = (*Adapter)(nil)
