package bootstrap

import (
	"fmt"
	"sync/atomic"
	"time"

	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type ClientFactory func(string, string, config.ModelProviderInfo) (llm.Client, error)

type Dependencies struct {
	ClientFactory    ClientFactory
	MCPClientFactory mcp.ClientFactory
	WebFetcher       webfetch.Fetcher
	WebSearch        websearch.Provider
	AuditFactory     AuditFactory
	ThreadStore      ThreadStoreFactory
	Clock            func() time.Time
	NextID           func(string) string
}

func DefaultDependencies(environment Environment) Dependencies {
	return Dependencies{
		AuditFactory: defaultAuditFactory(environment.LookupEnv),
		ThreadStore:  DefaultThreadStoreFactory,
		Clock:        time.Now,
		NextID:       NextPersistentID,
	}
}

func (dependencies Dependencies) sessionAdapters(modelMessages llm.ModelMessages) agentsession.ServiceAdapters {
	adapters := agentsession.ServiceAdapters{
		MCPClientFactory: dependencies.MCPClientFactory,
		WebFetcher:       dependencies.WebFetcher,
		WebSearch:        dependencies.WebSearch,
		AuditFactory:     agentsession.AuditFactory(dependencies.AuditFactory),
		ModelMessages:    modelMessages,
	}
	if dependencies.ClientFactory != nil {
		adapters.ClientFactory = func(providerName, model string, provider config.ModelProviderInfo) (llm.Client, error) {
			return dependencies.ClientFactory(providerName, model, provider)
		}
	}
	return adapters
}

var persistentIDSequence atomic.Uint64

func NextPersistentID(kind string) string {
	return fmt.Sprintf("%s-%d-%d", kind, time.Now().UTC().UnixNano(), persistentIDSequence.Add(1))
}
