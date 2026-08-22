package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestSlashAndSelectionUseSharedListVisual(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })

	_, slashModel := newTestFullscreen(t, nil)
	slashModel.palette = terminalPalette{
		Level: colorLevelTrueColor, Dark: true,
		Foreground: terminalRGB{225, 225, 225}, Background: terminalRGB{18, 18, 18},
	}
	slashModel.input.SetValue("/")
	slashModel.slashPopup.sync("/", false)
	slash := slashModel.slashPopupView()

	_, selectionModel := newTestFullscreen(t, nil)
	selectionModel.palette = slashModel.palette
	selectionModel.selection = &selectionOverlay{
		Title: "Resume Session", Subtitle: "Select a saved chat", Search: true,
		Items: []selectionItem{{Name: "session-1", Description: "Current project"}},
	}
	selection := selectionModel.inputBox()

	accentPrefix := strings.Split(slashModel.palette.selection().Render("X"), "X")[0]
	for name, rendered := range map[string]string{"slash": slash, "selection": selection} {
		if accentPrefix == "" || !strings.Contains(rendered, accentPrefix) {
			t.Fatalf("%s list omitted shared accent style: %q", name, rendered)
		}
		plain := xansi.Strip(rendered)
		if !strings.Contains(plain, "  ") {
			t.Fatalf("%s list omitted shared column spacing: %q", name, plain)
		}
		for _, forbidden := range []string{"╭", "╮", "╰", "╯"} {
			if strings.Contains(plain, forbidden) {
				t.Fatalf("%s list unexpectedly uses a box: %q", name, plain)
			}
		}
	}
	slashPlain := xansi.Strip(slash)
	if strings.Contains(slashPlain, "›") {
		t.Fatalf("slash popup retained modal selection cursor: %q", slashPlain)
	}
	if !strings.Contains(xansi.Strip(selection), "› session-1") {
		t.Fatalf("modal selection cursor missing: %q", xansi.Strip(selection))
	}
	for _, forbidden := range []string{"Commands", "↑/↓", "Enter insert", "Esc dismiss"} {
		if strings.Contains(slashPlain, forbidden) {
			t.Fatalf("slash popup unexpectedly contains %q: %q", forbidden, slashPlain)
		}
	}
	if !strings.Contains(xansi.Strip(selection), "Resume Session") {
		t.Fatalf("selection list title missing: %q", xansi.Strip(selection))
	}
}

func TestSlashPopupStylesSelectionWithoutCursorGlyph(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })

	_, model := newTestFullscreen(t, nil)
	model.palette = terminalPalette{
		Level: colorLevelTrueColor, Dark: true,
		Foreground: terminalRGB{225, 225, 225}, Background: terminalRGB{18, 18, 18},
	}
	model.input.SetValue("/e")
	model.slashPopup.sync("/e", false)
	rendered := model.slashPopupView()
	if strings.Contains(xansi.Strip(rendered), "›") {
		t.Fatalf("slash popup selection uses a cursor glyph: %q", xansi.Strip(rendered))
	}
	accentPrefix := strings.Split(model.palette.selection().Render("X"), "X")[0]
	if count := strings.Count(rendered, accentPrefix); count < 2 {
		t.Fatalf("selected command name/description are not styled together: count=%d output=%q", count, rendered)
	}
}

func TestSlashPopupReplacesFooterAndShowsOnlyPrefixMatches(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.session.ContextWindow = 128_000
	model.refreshStatusLine()
	model.input.SetValue("/e")
	model.slashPopup.sync("/e", false)

	input := xansi.Strip(model.inputBox())
	if strings.Contains(input, "/exit") {
		t.Fatalf("slash popup leaked into input surface: %q", input)
	}
	composer := xansi.Strip(model.composerView())
	if !strings.Contains(composer, "/exit") || strings.Contains(composer, "/skills") {
		t.Fatalf("/e popup = %q", composer)
	}
	if count := strings.Count(composer, "/exit"); count != 1 {
		t.Fatalf("/exit rendered %d times in composer: %q", count, composer)
	}
	if footer := model.composerFooterView(); footer != "" {
		t.Fatalf("slash popup retained footer: %q", xansi.Strip(footer))
	}
	view := xansi.Strip(model.View())
	if strings.Contains(view, "128K window") {
		t.Fatalf("slash popup view retained statusline: %q", view)
	}
}

func TestSelectionVisualBoundsRowsAndStylesDisabledItems(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	items := make([]selectionItem, 12)
	for index := range items {
		items[index] = selectionItem{Name: fmt.Sprintf("session-%d", index+1), Description: "Saved chat"}
	}
	items[6].Disabled = true
	items[6].DisabledReason = "unavailable while running"
	model.selection = &selectionOverlay{Title: "Sessions", Items: items, Selected: 6}
	rendered := xansi.Strip(model.renderSelectionOverlay(80))
	visibleRows := 0
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "session-") {
			visibleRows++
		}
	}
	if visibleRows != commandListMaxVisible {
		t.Fatalf("visible rows = %d, want %d: %q", visibleRows, commandListMaxVisible, rendered)
	}
	if !strings.Contains(rendered, "unavailable while running") || !strings.Contains(rendered, "› session-7") {
		t.Fatalf("disabled/selected projection missing: %q", rendered)
	}
}

func TestApprovalAndRenameUseSharedHeaderAndInputStyles(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.selection = &selectionOverlay{Title: "Rename session", Subtitle: "Type a name and press Enter", Input: true, Value: "new name", Hint: "Esc cancel"}
	rename := xansi.Strip(model.renderSelectionOverlay(80))
	for _, expected := range []string{"Rename session", "Esc cancel", "› new name"} {
		if !strings.Contains(rename, expected) {
			t.Fatalf("rename visual omitted %q: %q", expected, rename)
		}
	}
}
