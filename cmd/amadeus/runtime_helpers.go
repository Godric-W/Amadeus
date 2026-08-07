package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	openaiadapter "github.com/Godric-W/Amadeus/internal/llm/openai"
)

type llmClientFactory func(string, config.ProviderConfig) (llm.Client, error)
type runContextFactory func(context.Context) (context.Context, context.CancelFunc)

func defaultLLMClientFactory(providerName string, provider config.ProviderConfig) (llm.Client, error) {
	return openaiadapter.NewAdapter(providerName, provider)
}

func interruptibleTurnContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt)
}
