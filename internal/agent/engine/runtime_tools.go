package engine

import (
	"fmt"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/mcp"
	processdomain "github.com/Godric-W/Amadeus/internal/process"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/tool/builtin"
	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type ToolRuntimeOptions struct {
	Config           config.Config
	Project          project.Root
	Client           llm.Client
	Events           protocol.EventSink
	Audit            audit.Sink
	Skills           *skill.SkillCatalog
	MCP              *mcp.MCPRuntime
	WebFetcher       webfetch.Fetcher
	WebSearch        websearch.Provider
	FileSystemPolicy *project.FileSystemPolicy
}

type ToolRuntime struct {
	Registry   *tool.Registry
	Processes  *processdomain.Manager
	Visibility map[string]bool
	WebFetcher webfetch.Fetcher
	WebSearch  websearch.Provider
}

func BuildToolRuntime(options ToolRuntimeOptions) (ToolRuntime, error) {
	processes := processdomain.NewManager()
	coreOptions := builtin.DefaultCoreToolOptions()
	coreOptions.FileSystemPolicy = options.FileSystemPolicy
	coreOptions.Events = options.Events
	coreOptions.ExecuteCommand.Audit = options.Audit
	coreOptions.ExecuteCommand.ProcessManager = processes
	coreOptions.ExecuteCommand.SkillCatalog = options.Skills
	registry, err := builtin.NewCoreRegistry(options.Project, coreOptions)
	if err != nil {
		processes.Close()
		return ToolRuntime{}, fmt.Errorf("create core tool registry: %w", err)
	}
	visibility := make(map[string]bool)
	if options.Client.Capabilities().SupportsImages {
		viewImage, createErr := builtin.NewViewImage(options.Project, builtin.ViewImageOptions{FileSystemPolicy: options.FileSystemPolicy})
		if createErr != nil {
			processes.Close()
			return ToolRuntime{}, fmt.Errorf("create view_image tool: %w", createErr)
		}
		if err := registry.RegisterDefinitionWithRegistration(viewImage, tool.Registration{Exposure: tool.ExposureConditional, Condition: "provider.images"}); err != nil {
			processes.Close()
			return ToolRuntime{}, err
		}
		visibility["provider.images"] = true
	}
	if options.Config.Web.Fetch.Enabled {
		if options.WebFetcher == nil {
			options.WebFetcher, err = webfetch.New(webfetch.Options{MaxBytes: options.Config.Web.Fetch.MaxBytes, MaxRedirects: options.Config.Web.Fetch.MaxRedirects, Timeout: options.Config.Web.Fetch.Timeout})
			if err != nil {
				processes.Close()
				return ToolRuntime{}, err
			}
		}
		definition, createErr := builtin.NewWebFetch(options.WebFetcher)
		if createErr != nil {
			processes.Close()
			return ToolRuntime{}, createErr
		}
		if err := registry.RegisterDefinitionWithRegistration(definition, tool.Registration{Exposure: tool.ExposureConditional, Condition: "web.fetch.configured"}); err != nil {
			processes.Close()
			return ToolRuntime{}, err
		}
		visibility["web.fetch.configured"] = true
	}
	if options.Config.Web.Search.Enabled {
		if options.WebSearch == nil {
			provider, createErr := websearch.NewProvider(websearch.ProviderOptions{Name: string(options.Config.Web.Search.Provider), APIKey: options.Config.Web.Search.APIKey, BaseURL: options.Config.Web.Search.BaseURL})
			if createErr != nil {
				processes.Close()
				return ToolRuntime{}, createErr
			}
			options.WebSearch, err = websearch.NewService(provider, websearch.ServiceOptions{Timeout: options.Config.Web.Search.Timeout, MaxResults: options.Config.Web.Search.MaxResults})
			if err != nil {
				processes.Close()
				return ToolRuntime{}, err
			}
		}
		definition, createErr := builtin.NewWebSearch(options.WebSearch)
		if createErr != nil {
			processes.Close()
			return ToolRuntime{}, createErr
		}
		if err := registry.RegisterDefinitionWithRegistration(definition, tool.Registration{Exposure: tool.ExposureConditional, Condition: "web.search.configured"}); err != nil {
			processes.Close()
			return ToolRuntime{}, err
		}
		visibility["web.search.configured"] = true
	}
	if options.Skills != nil && options.Skills.Len() > 0 {
		definition, createErr := builtin.NewReadSkill(options.Skills, builtin.ReadSkillOptions{})
		if createErr != nil {
			processes.Close()
			return ToolRuntime{}, createErr
		}
		if err := registry.RegisterDefinitionWithRegistration(definition, tool.Registration{Exposure: tool.ExposureConditional, Condition: "skills.available"}); err != nil {
			processes.Close()
			return ToolRuntime{}, err
		}
		visibility["skills.available"] = true
	}
	mcpList, mcpCall, err := mcp.NewLazyTools(options.MCP)
	if err != nil {
		processes.Close()
		return ToolRuntime{}, err
	}
	if mcpList != nil {
		if err := registry.RegisterDefinitionWithRegistration(mcpList, tool.Registration{Exposure: tool.ExposureConditional, Condition: "mcp.configured"}); err != nil {
			processes.Close()
			return ToolRuntime{}, err
		}
	}
	if mcpCall != nil {
		if err := registry.RegisterDefinitionWithRegistration(mcpCall, tool.Registration{Exposure: tool.ExposureDeferred, Condition: "mcp.catalog"}); err != nil {
			processes.Close()
			return ToolRuntime{}, err
		}
	}
	listResources, readResource, err := mcp.NewResourceTools(options.MCP)
	if err != nil {
		processes.Close()
		return ToolRuntime{}, err
	}
	if listResources != nil {
		if err := registry.RegisterDefinitionWithRegistration(listResources, tool.Registration{Exposure: tool.ExposureConditional, Condition: "mcp.resources"}); err != nil {
			processes.Close()
			return ToolRuntime{}, err
		}
	}
	if readResource != nil {
		if err := registry.RegisterDefinitionWithRegistration(readResource, tool.Registration{Exposure: tool.ExposureDeferred, Condition: "mcp.resources"}); err != nil {
			processes.Close()
			return ToolRuntime{}, err
		}
	}
	if options.MCP != nil && len(options.MCP.EnabledServers()) > 0 {
		visibility["mcp.configured"] = true
		visibility["mcp.catalog"] = true
		visibility["mcp.resources"] = true
	}
	return ToolRuntime{Registry: registry, Processes: processes, Visibility: visibility, WebFetcher: options.WebFetcher, WebSearch: options.WebSearch}, nil
}
