package tui

import (
	"reflect"
	"strings"
	"testing"
)

func TestSlashCommandCatalogMatchesM9Contract(t *testing.T) {
	want := []string{"/resume", "/skills", "/rename", "/delete", "/compact", "/plan", "/copy", "/status", "/mcp", "/clear", "/exit"}
	if got := SlashCommands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Slash commands = %v, want %v", got, want)
	}
	formatted := FormatSlashCatalog()
	for _, command := range want {
		if !strings.Contains(formatted, command+"  ") {
			t.Fatalf("formatted catalog omitted %q: %q", command, formatted)
		}
	}
	for _, removed := range []string{"/help", "/sessions", "/tools"} {
		if strings.Contains(formatted, removed) {
			t.Fatalf("formatted catalog retained removed command %q: %q", removed, formatted)
		}
	}
}

func TestSlashCommandFilteringAndValidation(t *testing.T) {
	if matches := FilterSlashCommands("/re", false); len(matches) != 2 || matches[0].Command != SlashResume || matches[1].Command != SlashRename {
		t.Fatalf("prefix matches = %#v", matches)
	}
	if matches := FilterSlashCommands("/mcp verbose", false); len(matches) != 0 {
		t.Fatalf("argument input unexpectedly retained popup: %#v", matches)
	}
	spec, arguments, ok := ParseSlashCommand("/mcp verbose")
	if !ok || spec.Command != SlashMCP || arguments != "verbose" || ValidateSlashCommandArguments(spec, arguments) != nil {
		t.Fatalf("valid MCP command rejected: spec=%#v args=%q ok=%v", spec, arguments, ok)
	}
	if err := ValidateSlashCommandArguments(spec, "details"); err == nil {
		t.Fatal("invalid MCP argument unexpectedly accepted")
	}
	plan, _ := FindSlashCommand("plan")
	if err := ValidateSlashCommandArguments(plan, "task"); err == nil {
		t.Fatal("/plan argument unexpectedly accepted")
	}
}

func TestSlashPopupCyclesCompletesAndDismisses(t *testing.T) {
	var popup slashCommandPopup
	popup.sync("/re", false)
	if !popup.active() || popup.selected != 0 {
		t.Fatalf("popup did not activate: %#v", popup)
	}
	popup.move(-1)
	selected, ok := popup.selectedItem()
	if !ok || selected.Command != SlashRename {
		t.Fatalf("popup did not wrap: %#v", selected)
	}
	popup.dismiss("/re")
	popup.sync("/re", false)
	if popup.active() {
		t.Fatal("dismissed popup reopened without input change")
	}
	popup.resetDismissal("/res")
	popup.sync("/res", false)
	if selected, ok = popup.selectedItem(); !ok || selected.Command != SlashResume {
		t.Fatalf("popup did not reopen for changed input: %#v", popup)
	}
}

func TestSlashPopupBoundsVisibleRowsAroundSelection(t *testing.T) {
	var popup slashCommandPopup
	popup.sync("/", false)
	for popup.selected < len(popup.items)-1 {
		popup.move(1)
	}
	visible, start := popup.visibleItems()
	if len(visible) != slashPopupMaxVisible || start == 0 || start+len(visible)-1 != popup.selected {
		t.Fatalf("visible popup window: start=%d selected=%d items=%d", start, popup.selected, len(visible))
	}
}

func TestSelectionOverlaySearchUsesSourceIndices(t *testing.T) {
	overlay := &selectionOverlay{Search: true, Value: "second", Items: []selectionItem{
		{Name: "session-1", Description: "first"},
		{Name: "session-2", Description: "second"},
		{Name: "session-3", Description: "third"},
	}}
	index, ok := overlay.selectedIndex()
	if !ok || index != 1 {
		t.Fatalf("filtered selection index = %d, ok=%v", index, ok)
	}
}
