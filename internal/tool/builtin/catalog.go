package builtin

import "github.com/Godric-W/Amadeus/internal/tool"

type CatalogStatus string

const (
	CatalogAvailable CatalogStatus = "available"
	CatalogMigration CatalogStatus = "migration"
	CatalogPlanned   CatalogStatus = "planned"
)

type CatalogEntry struct {
	Name       string
	Exposure   tool.Exposure
	Condition  string
	Status     CatalogStatus
	SideEffect tool.SideEffect
}

func TargetCatalog() []CatalogEntry {
	entries := []CatalogEntry{
		{Name: "read", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectRead},
		{Name: "edit", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectWrite},
		{Name: "write", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectWrite},
		{Name: "glob", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectRead},
		{Name: "grep", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectRead},
		{Name: "execute_command", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectExecute},
		{Name: "update_plan", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectNone},
		{Name: "request_user_input", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectNone},
		{Name: "write_stdin", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectExecute},
		{Name: "spawn_agent", Exposure: tool.ExposureConditional, Condition: "multi_agent.enabled", Status: CatalogAvailable, SideEffect: tool.SideEffectNone},
		{Name: "send_input", Exposure: tool.ExposureConditional, Condition: "multi_agent.enabled", Status: CatalogAvailable, SideEffect: tool.SideEffectNone},
		{Name: "wait_agent", Exposure: tool.ExposureConditional, Condition: "multi_agent.enabled", Status: CatalogAvailable, SideEffect: tool.SideEffectNone},
		{Name: "close_agent", Exposure: tool.ExposureConditional, Condition: "multi_agent.enabled", Status: CatalogAvailable, SideEffect: tool.SideEffectNone},
		{Name: "view_image", Exposure: tool.ExposureConditional, Condition: "model.image_input", Status: CatalogAvailable, SideEffect: tool.SideEffectRead},
		{Name: "read_skill", Exposure: tool.ExposureConditional, Condition: "skills.available", Status: CatalogAvailable, SideEffect: tool.SideEffectRead},
		{Name: "web_search", Exposure: tool.ExposureConditional, Condition: "web.search.configured", Status: CatalogAvailable, SideEffect: tool.SideEffectNetwork},
		{Name: "web_fetch", Exposure: tool.ExposureConditional, Condition: "web.fetch.configured", Status: CatalogAvailable, SideEffect: tool.SideEffectNetwork},
		{Name: "mcp_list_tools", Exposure: tool.ExposureConditional, Condition: "mcp.configured", Status: CatalogAvailable, SideEffect: tool.SideEffectNetwork},
		{Name: "mcp_call", Exposure: tool.ExposureDeferred, Condition: "mcp.catalog", Status: CatalogAvailable, SideEffect: tool.SideEffectNetwork},
		{Name: "mcp_list_resources", Exposure: tool.ExposureConditional, Condition: "mcp.resources", Status: CatalogAvailable, SideEffect: tool.SideEffectNetwork},
		{Name: "mcp_read_resource", Exposure: tool.ExposureDeferred, Condition: "mcp.resources", Status: CatalogAvailable, SideEffect: tool.SideEffectNetwork},
	}
	return append([]CatalogEntry(nil), entries...)
}
