package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	openaiadapter "github.com/Godric-W/Amadeus/internal/llm/openai"
)

type llmClientFactory func(string, string, config.ModelProviderInfo) (llm.Client, error)
type turnContextFactory func(context.Context) (context.Context, context.CancelFunc)

func defaultLLMClientFactory(providerName, model string, provider config.ModelProviderInfo) (llm.Client, error) {
	return openaiadapter.NewAdapter(providerName, model, provider)
}

func interruptibleTurnContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt)
}
