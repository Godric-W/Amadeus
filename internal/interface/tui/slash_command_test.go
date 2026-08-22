package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestBuiltinSlashCommandsMatchesCodexContract(t *testing.T) {
	want := []string{"/resume", "/skills", "/rename", "/delete", "/compact", "/plan", "/copy", "/status", "/mcp", "/clear", "/exit"}
	if got := SlashCommands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Slash commands = %v, want %v", got, want)
	}
	formatted := FormatSlashCommands()
	for _, command := range want {
		if !strings.Contains(formatted, command+"  ") {
			t.Fatalf("formatted commands omitted %q: %q", command, formatted)
		}
	}
	for _, removed := range []string{"/help", "/sessions", "/tools"} {
		if strings.Contains(formatted, removed) {
			t.Fatalf("formatted commands retained removed command %q", removed)
		}
	}
}

func TestSlashCommandFilteringAndValidation(t *testing.T) {
	if matches := FilterSlashCommands("/e", false); len(matches) != 1 || matches[0] != SlashExit {
		t.Fatalf("/e matches = %#v, want only /exit", matches)
	}
	if matches := FilterSlashCommands("/re", false); len(matches) != 2 || matches[0] != SlashResume || matches[1] != SlashRename {
		t.Fatalf("prefix matches = %#v", matches)
	}
	if matches := FilterSlashCommands("/resu", true); len(matches) != 1 || matches[0] != SlashResume {
		t.Fatalf("running /resu matches = %#v, want only /resume", matches)
	}
	if matches := FilterSlashCommands("/mcp verbose", false); len(matches) != 0 {
		t.Fatalf("argument input unexpectedly retained popup: %#v", matches)
	}
	invocation, err := ParseSlashInvocation("/mcp verbose")
	if err != nil || invocation.Command != SlashMCP || invocation.Args != "verbose" {
		t.Fatalf("valid MCP command rejected: invocation=%#v err=%v", invocation, err)
	}
	if _, err := ParseSlashInvocation("/mcp details"); err == nil {
		t.Fatal("invalid MCP argument unexpectedly accepted")
	}
	invocation, err = ParseSlashInvocation("/plan task")
	if err != nil || invocation.Command != SlashPlan || invocation.Args != "task" {
		t.Fatalf("/plan task parse = %#v err=%v", invocation, err)
	}
	parsed, err := ParseInput("inspect repository")
	if err != nil || parsed.Text != "inspect repository" || parsed.Command != nil {
		t.Fatalf("plain input parse = %#v err=%v", parsed, err)
	}
}

func TestSlashPopupShowsResumeForTypedPrefixDuringTask(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	for _, value := range "/resu" {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}})
		model = updated.(fullscreenModel)
	}
	selected, ok := model.slashPopup.selectedItem()
	if model.input.Value() != "/resu" || !ok || selected != SlashResume {
		t.Fatalf("input=%q popup=%#v selected=%q ok=%v", model.input.Value(), model.slashPopup, selected, ok)
	}
	if rendered := xansi.Strip(model.View()); !strings.Contains(rendered, "/resume") {
		t.Fatalf("/resu popup omitted /resume: %q", rendered)
	}
}

func TestSlashPopupShowsResumeForBatchedRuneInput(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/resu")})
	model = updated.(fullscreenModel)
	selected, ok := model.slashPopup.selectedItem()
	if model.input.Value() != "/resu" || !ok || selected != SlashResume {
		t.Fatalf("input=%q popup=%#v selected=%q ok=%v", model.input.Value(), model.slashPopup, selected, ok)
	}
	if rendered := xansi.Strip(model.View()); !strings.Contains(rendered, "/resume") {
		t.Fatalf("batched /resu popup omitted /resume: %q", rendered)
	}
}

func TestSlashPopupKeepsStableRowsWhilePrefixNarrows(t *testing.T) {
	for _, test := range []struct {
		prefix string
		next   rune
		want   string
	}{
		{prefix: "/re", next: 's', want: "/resume"},
		{prefix: "/s", next: 'k', want: "/skills"},
	} {
		t.Run(test.prefix+string(test.next), func(t *testing.T) {
			_, model := newTestFullscreen(t, nil)
			for _, value := range test.prefix {
				updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}})
				model = updated.(fullscreenModel)
			}
			before := lipgloss.Height(model.composerView())
			updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{test.next}})
			model = updated.(fullscreenModel)
			after := lipgloss.Height(model.composerView())
			if before != after {
				t.Fatalf("composer height changed while popup narrowed: before=%d after=%d", before, after)
			}
			if rendered := xansi.Strip(model.composerView()); !strings.Contains(rendered, test.want) {
				t.Fatalf("narrowed popup omitted %q: %q", test.want, rendered)
			}
		})
	}
}

func TestSlashPopupPrefixChangeResetsSelection(t *testing.T) {
	var popup slashCommandPopup
	popup.sync("/", false)
	popup.move(1)
	if selected, _ := popup.selectedItem(); selected != SlashSkills {
		t.Fatalf("test setup selected %q, want /skills", selected)
	}
	popup.sync("/e", false)
	if selected, ok := popup.selectedItem(); !ok || selected != SlashExit || popup.selected != 0 {
		t.Fatalf("/e popup selected=%q index=%d ok=%v", selected, popup.selected, ok)
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
	if !ok || selected != SlashRename {
		t.Fatalf("popup did not wrap: %#v", selected)
	}
	popup.dismiss("/re")
	popup.sync("/re", false)
	if popup.active() {
		t.Fatal("dismissed popup reopened without input change")
	}
	popup.resetDismissal("/res")
	popup.sync("/res", false)
	if selected, ok = popup.selectedItem(); !ok || selected != SlashResume {
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
		t.Fatalf("visible popup window: start=%d selected=%d items=%d", start, popup.selected, len(popup.items))
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
