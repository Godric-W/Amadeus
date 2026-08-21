package llm

import (
	"context"
	"errors"
)

type configuredClient struct {
	client Client
	model  ModelInfo
}

func WithModelInfo(client Client, model ModelInfo) (Client, error) {
	if client == nil {
		return nil, errors.New("configured model client is nil")
	}
	model = model.Normalized()
	if model.Name == "" {
		return nil, errors.New("configured model name is empty")
	}
	return &configuredClient{client: client, model: model}, nil
}

func (client *configuredClient) Complete(ctx context.Context, request Request) (Response, error) {
	return client.client.Complete(ctx, request)
}

func (client *configuredClient) Stream(ctx context.Context, request Request) (Stream, error) {
	return client.client.Stream(ctx, request)
}

func (client *configuredClient) Model() ModelInfo { return client.model }

func (client *configuredClient) Capabilities() Capabilities { return client.client.Capabilities() }
