package builtin

import (
	"testing"

	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestTargetCatalogFixesCoreConditionalDeferredAndHiddenTools(t *testing.T) {
	entries := TargetCatalog()
	if len(entries) != 19 {
		t.Fatalf("unexpected target catalog size: %d", len(entries))
	}
	byName := make(map[string]CatalogEntry, len(entries))
	for _, entry := range entries {
		if _, exists := byName[entry.Name]; exists {
			t.Fatalf("duplicate catalog tool %q", entry.Name)
		}
		byName[entry.Name] = entry
	}
	checks := map[string]tool.Exposure{
		"read":       tool.ExposureDirect,
		"edit":       tool.ExposureDirect,
		"write":      tool.ExposureDirect,
		"glob":       tool.ExposureDirect,
		"grep":       tool.ExposureDirect,
		"view_image": tool.ExposureConditional,
		"mcp_call":   tool.ExposureDeferred,
	}
	for name, exposure := range checks {
		if byName[name].Exposure != exposure {
			t.Fatalf("tool %q exposure = %q, want %q", name, byName[name].Exposure, exposure)
		}
	}
}
