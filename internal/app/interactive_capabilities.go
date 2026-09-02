package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/protocol"
)

func (application *InteractiveApplication) Status() StatusSnapshot {
	active, _, err := application.current()
	if err != nil {
		info := &protocol.TokenUsageInfo{ModelContextWindow: application.configuration.Runtime.ModelContextWindow}
		return StatusSnapshot{
			CurrentDir: application.configuration.CWD, Provider: application.configuration.Runtime.ModelProvider,
			Model: application.configuration.Runtime.Model, ReasoningEffort: llm.CloneReasoningEffort(application.configuration.Runtime.ModelReasoningEffort),
			Mode: application.configuration.Mode, Phase: "unavailable", TokenInfo: info,
		}
	}
	application.mu.RLock()
	title, phase, usage, goal := application.title, application.phase, application.usage, cloneThreadGoal(application.goal)
	application.mu.RUnlock()
	configuration := active.Configuration()
	result := StatusSnapshot{
		SessionID: active.SessionID(), ThreadID: active.ID(), Title: title, CurrentDir: configuration.CWD,
		Provider: configuration.Provider, Model: configuration.Model,
		ReasoningEffort: llm.CloneReasoningEffort(configuration.ReasoningEffort),
		Mode:            configuration.Mode, Phase: phase,
		TokenInfo: cloneTokenInfo(usage.Info), Goal: goal, ActiveContextTokens: usage.ActiveContextTokens,
		ActiveContextEstimated: usage.ActiveContextEstimated, RolloutItems: active.RolloutItemCount(),
	}
	result.PermissionGrantCount = active.PermissionGrantCount()
	result.SkillRevision = shortRevision(active.SkillRevision())
	result.MCPRevision = shortRevision(active.MCPRevision())
	prompt := active.PromptDiagnostics()
	result.BaseProvenance = prompt.BaseProvenance
	result.WorldStateKind = prompt.WorldStateKind
	result.WorldStateRevision = shortRevision(prompt.WorldStateRevision)
	result.InstructionsRevision = shortRevision(prompt.InstructionsRevision)
	result.CollaborationRevision = shortRevision(prompt.CollaborationRevision)
	result.MultiAgentRevision = shortRevision(prompt.MultiAgentRevision)
	result.CompactionRevision = shortRevision(prompt.CompactionRevision)
	result.SummaryPrefixRevision = shortRevision(prompt.SummaryPrefixRevision)
	result.ProviderWireAPI = prompt.ProviderWireAPI
	return result
}

func (application *InteractiveApplication) LoadMCP(ctx context.Context, requestID uint64, detail MCPDetail) {
	active, generation, err := application.current()
	if err != nil {
		application.emit(MCPInventoryLoaded{RequestID: requestID, Detail: detail, Error: err})
		return
	}
	result := MCPInventoryLoaded{RequestID: requestID, Generation: generation, ThreadID: active.ID(), Detail: detail}
	configured := active.MCPConfiguration()
	servers := make([]string, 0, len(configured.Servers))
	for server := range configured.Servers {
		servers = append(servers, server)
	}
	sort.Strings(servers)
	for _, server := range servers {
		serverConfig := configured.Servers[server]
		status := MCPServerStatus{Name: server, Enabled: serverConfig.IsEnabled(), AuthStatus: mcpAuthStatus(serverConfig)}
		if !status.Enabled {
			result.Inventory.Servers = append(result.Inventory.Servers, status)
			continue
		}
		catalog, listErr := active.MCPTools(ctx, server)
		if listErr != nil {
			status.Error = listErr.Error()
		} else {
			status.Tools = append(status.Tools, catalog.Tools...)
		}
		if detail == MCPDetailVerbose {
			resources, resourceErr := active.MCPResources(ctx, server)
			if resourceErr != nil {
				if status.Error == "" {
					status.Error = resourceErr.Error()
				} else {
					status.Error += "; " + resourceErr.Error()
				}
			} else {
				status.Resources = append(status.Resources, resources.Resources...)
			}
		}
		result.Inventory.Servers = append(result.Inventory.Servers, status)
	}
	application.emit(result)
}

func mcpAuthStatus(server mcp.ServerConfig) MCPAuthStatus {
	for name, value := range server.Headers {
		if strings.EqualFold(strings.TrimSpace(name), "Authorization") && strings.TrimSpace(value) != "" {
			return MCPAuthBearerToken
		}
	}
	if server.Transport == mcp.TransportStreamableHTTP {
		return MCPAuthUnsupported
	}
	return MCPAuthUnknown
}

func (application *InteractiveApplication) LoadSkills() {
	active, generation, err := application.current()
	if err != nil {
		application.emit(SkillsLoaded{Error: err})
		return
	}
	metadata := active.Skills()
	values := make([]SkillOption, 0, len(metadata))
	for _, value := range metadata {
		values = append(values, SkillOption{Name: value.Name, Description: value.Description, Source: string(value.Source), Path: value.PathToSkillMD, Enabled: value.Enabled})
	}
	application.emit(SkillsLoaded{Generation: generation, Skills: values})
}

func (application *InteractiveApplication) SetSkillEnabled(path string, enabled bool) {
	active, generation, err := application.current()
	if err != nil {
		application.emit(SkillEnabledSet{Generation: generation, Path: path, Enabled: enabled, Error: err})
		return
	}
	name := ""
	for _, value := range active.Skills() {
		if value.PathToSkillMD == path {
			name = value.Name
			break
		}
	}
	if name == "" {
		application.emit(SkillEnabledSet{Generation: generation, Path: path, Enabled: enabled, Error: fmt.Errorf("skill path %q is unavailable", path)})
		return
	}
	err = active.SetSkillEnabled(name, enabled)
	application.emit(SkillEnabledSet{Generation: generation, Path: path, Enabled: enabled, Error: err})
}

func shortRevision(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}
