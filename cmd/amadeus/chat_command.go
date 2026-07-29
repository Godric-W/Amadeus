package main

import (
	"context"
	"os"
	"os/signal"

	agentruntime "github.com/Godric-W/Amadeus/internal/agent/runtime"
	"github.com/Godric-W/Amadeus/internal/config"
	interfacecli "github.com/Godric-W/Amadeus/internal/interface/cli"
	"github.com/Godric-W/Amadeus/internal/llm"
	openaiadapter "github.com/Godric-W/Amadeus/internal/llm/openai"
	"github.com/Godric-W/Amadeus/internal/render"
	"github.com/spf13/cobra"
)

type llmClientFactory func(string, config.ProviderConfig) (llm.Client, error)
type chatTurnContextFactory = interfacecli.TurnContextFactory

func newChatCommand(flags *configFlags, commandRuntime commandRuntime) *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Start a streaming chat session",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			configured, _, err := loadEffectiveConfig(command, flags, commandRuntime)
			if err != nil {
				return err
			}
			if err := config.Validate(configured); err != nil {
				return err
			}

			providerName := configured.DefaultProvider
			provider := configured.Providers[providerName]
			factory := commandRuntime.llmClientFactory
			if factory == nil {
				factory = defaultLLMClientFactory
			}
			client, err := factory(providerName, provider)
			if err != nil {
				return err
			}
			renderer, err := render.NewPlainRenderer(command.OutOrStdout(), command.ErrOrStderr())
			if err != nil {
				return err
			}
			session, err := agentruntime.NewSession(client, renderer, agentruntime.SessionOptions{
				Temperature:     provider.Temperature,
				MaxOutputTokens: provider.MaxOutputTokens,
			})
			if err != nil {
				return err
			}
			loop, err := interfacecli.NewChatLoop(command.InOrStdin(), session)
			if err != nil {
				return err
			}
			turnContextFactory := commandRuntime.turnContextFactory
			if turnContextFactory == nil {
				turnContextFactory = interruptibleTurnContext
			}
			return loop.WithTurnContextFactory(turnContextFactory).Run(command.Context())
		},
	}
}

func defaultLLMClientFactory(providerName string, provider config.ProviderConfig) (llm.Client, error) {
	return openaiadapter.NewAdapter(providerName, provider)
}

func interruptibleTurnContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt)
}
