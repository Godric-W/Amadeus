package engine

import (
	"fmt"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	extensionruntime "github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/mcp"
	processdomain "github.com/Godric-W/Amadeus/internal/process"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/tool/builtin"
	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type toolRuntimeOptions struct {
	configured        config.Config
	project           project.Root
	client            llm.Client
	events            protocol.EventSink
	planUpdater       builtin.PlanUpdater
	audit             audit.Sink
	extensionAssembly *extensionruntime.Assembly
	webFetcher        webfetch.Fetcher
	webSearch         websearch.Provider
	fileSystemPolicy  *project.FileSystemPolicy
}

func buildToolRuntime(options toolRuntimeOptions) (*tool.Registry, *processdomain.Manager, map[string]bool, error) {
	processes := processdomain.NewManager()
	coreOptions := builtin.DefaultCoreToolOptions()
	coreOptions.FileSystemPolicy = options.fileSystemPolicy
	coreOptions.Events = options.events
	coreOptions.ExecuteCommand.Audit = options.audit
	coreOptions.ExecuteCommand.ProcessManager = processes
	coreOptions.ExecuteCommand.SkillCatalog = options.extensionAssembly.SkillCatalog()
	coreOptions.PlanUpdater = options.planUpdater
	registry, err := builtin.NewCoreRegistry(options.project, coreOptions)
	if err != nil {
		processes.Close()
		return nil, nil, nil, fmt.Errorf("create core tool registry: %w", err)
	}
	visibility := make(map[string]bool)
	if options.client.Capabilities().SupportsImages {
		viewImage, createErr := builtin.NewViewImage(options.project, builtin.ViewImageOptions{FileSystemPolicy: options.fileSystemPolicy})
		if createErr != nil {
			processes.Close()
			return nil, nil, nil, fmt.Errorf("create view_image tool: %w", createErr)
		}
		if err := registry.RegisterDefinitionWithRegistration(viewImage, tool.Registration{Exposure: tool.ExposureConditional, Condition: "provider.images"}); err != nil {
			processes.Close()
			return nil, nil, nil, err
		}
		visibility["provider.images"] = true
	}
	if options.configured.Web.Fetch.Enabled {
		if options.webFetcher == nil {
			options.webFetcher, err = webfetch.New(webfetch.Options{MaxBytes: options.configured.Web.Fetch.MaxBytes, MaxRedirects: options.configured.Web.Fetch.MaxRedirects, Timeout: options.configured.Web.Fetch.Timeout})
			if err != nil {
				processes.Close()
				return nil, nil, nil, err
			}
		}
		definition, createErr := builtin.NewWebFetch(options.webFetcher)
		if createErr != nil {
			processes.Close()
			return nil, nil, nil, createErr
		}
		if err := registry.RegisterDefinitionWithRegistration(definition, tool.Registration{Exposure: tool.ExposureConditional, Condition: "web.fetch.configured"}); err != nil {
			processes.Close()
			return nil, nil, nil, err
		}
		visibility["web.fetch.configured"] = true
	}
	if options.configured.Web.Search.Enabled {
		if options.webSearch == nil {
			provider, createErr := websearch.NewProvider(websearch.ProviderOptions{Name: string(options.configured.Web.Search.Provider), APIKey: options.configured.Web.Search.APIKey, BaseURL: options.configured.Web.Search.BaseURL})
			if createErr != nil {
				processes.Close()
				return nil, nil, nil, createErr
			}
			options.webSearch, err = websearch.NewService(provider, websearch.ServiceOptions{Timeout: options.configured.Web.Search.Timeout, MaxResults: options.configured.Web.Search.MaxResults})
			if err != nil {
				processes.Close()
				return nil, nil, nil, err
			}
		}
		definition, createErr := builtin.NewWebSearch(options.webSearch)
		if createErr != nil {
			processes.Close()
			return nil, nil, nil, createErr
		}
		if err := registry.RegisterDefinitionWithRegistration(definition, tool.Registration{Exposure: tool.ExposureConditional, Condition: "web.search.configured"}); err != nil {
			processes.Close()
			return nil, nil, nil, err
		}
		visibility["web.search.configured"] = true
	}
	if skills := options.extensionAssembly.SkillCatalog(); skills != nil && skills.Len() > 0 {
		definition, createErr := builtin.NewReadSkill(skills, builtin.ReadSkillOptions{})
		if createErr != nil {
			processes.Close()
			return nil, nil, nil, createErr
		}
		if err := registry.RegisterDefinitionWithRegistration(definition, tool.Registration{Exposure: tool.ExposureConditional, Condition: "skills.available"}); err != nil {
			processes.Close()
			return nil, nil, nil, err
		}
		visibility["skills.available"] = true
	}
	mcpRuntime := options.extensionAssembly.MCPRuntime()
	mcpList, mcpCall, err := mcp.NewLazyTools(mcpRuntime)
	if err != nil {
		processes.Close()
		return nil, nil, nil, err
	}
	if mcpList != nil {
		if err := registry.RegisterDefinitionWithRegistration(mcpList, tool.Registration{Exposure: tool.ExposureConditional, Condition: "mcp.configured"}); err != nil {
			processes.Close()
			return nil, nil, nil, err
		}
	}
	if mcpCall != nil {
		if err := registry.RegisterDefinitionWithRegistration(mcpCall, tool.Registration{Exposure: tool.ExposureDeferred, Condition: "mcp.catalog"}); err != nil {
			processes.Close()
			return nil, nil, nil, err
		}
	}
	listResources, readResource, err := mcp.NewResourceTools(mcpRuntime)
	if err != nil {
		processes.Close()
		return nil, nil, nil, err
	}
	if listResources != nil {
		if err := registry.RegisterDefinitionWithRegistration(listResources, tool.Registration{Exposure: tool.ExposureConditional, Condition: "mcp.resources"}); err != nil {
			processes.Close()
			return nil, nil, nil, err
		}
	}
	if readResource != nil {
		if err := registry.RegisterDefinitionWithRegistration(readResource, tool.Registration{Exposure: tool.ExposureDeferred, Condition: "mcp.resources"}); err != nil {
			processes.Close()
			return nil, nil, nil, err
		}
	}
	if mcpRuntime != nil && len(mcpRuntime.EnabledServers()) > 0 {
		visibility["mcp.configured"] = true
		visibility["mcp.catalog"] = true
		visibility["mcp.resources"] = true
	}
	return registry, processes, visibility, nil
}
