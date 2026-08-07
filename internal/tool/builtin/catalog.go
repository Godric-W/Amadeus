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
		{Name: "apply_patch", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectWrite},
		{Name: "execute_command", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectExecute},
		{Name: "glob_files", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectRead},
		{Name: "grep_code", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectRead},
		{Name: "list_dir", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectRead},
		{Name: "read_file", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectRead},
		{Name: "request_permissions", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectNone},
		{Name: "update_plan", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectNone},
		{Name: "write_stdin", Exposure: tool.ExposureDirect, Status: CatalogAvailable, SideEffect: tool.SideEffectExecute},
		{Name: "view_image", Exposure: tool.ExposureConditional, Condition: "provider.images", Status: CatalogAvailable, SideEffect: tool.SideEffectRead},
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
