package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestTerminalPaletteSemanticMatrix(t *testing.T) {
	original := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })
	for _, test := range []struct {
		name    string
		palette terminalPalette
		profile termenv.Profile
		ansi    bool
	}{
		{name: "true-color-dark", palette: terminalPalette{Level: colorLevelTrueColor, Dark: true, Foreground: terminalRGB{225, 225, 225}, Background: terminalRGB{18, 18, 18}}, profile: termenv.TrueColor, ansi: true},
		{name: "ansi256-light", palette: terminalPalette{Level: colorLevelANSI256, Dark: false}, profile: termenv.ANSI256, ansi: true},
		{name: "ansi16-dark", palette: terminalPalette{Level: colorLevelANSI16, Dark: true}, profile: termenv.ANSI, ansi: true},
		{name: "no-color", palette: terminalPalette{Level: colorLevelNone, NoColor: true}, profile: termenv.Ascii, ansi: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			lipgloss.SetColorProfile(test.profile)
			styles := []string{
				test.palette.accent().Render("accent"), test.palette.success().Render("success"),
				test.palette.failure().Render("failure"), test.palette.warning().Render("warning"), test.palette.command().Render("command"), test.palette.dim().Render("dim"),
			}
			for _, rendered := range styles {
				if strings.Contains(rendered, "\x1b[") != test.ansi {
					t.Fatalf("ANSI=%v rendered=%q", test.ansi, rendered)
				}
			}
		})
	}
}

func TestCommandPaletteUsesCodexMagenta(t *testing.T) {
	original := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })
	lipgloss.SetColorProfile(termenv.ANSI)
	rendered := terminalPalette{Level: colorLevelANSI16, Dark: true}.command().Render("/mcp")
	if !strings.Contains(rendered, "\x1b[35m") {
		t.Fatalf("command color = %q", rendered)
	}
}

func TestTurnSeparatorUsesCodexDimModifier(t *testing.T) {
	original := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	rendered := terminalPalette{Level: colorLevelTrueColor, Dark: true}.turnSeparator().Render("Worked for 1m 22s")
	if !strings.Contains(rendered, "\x1b[2m") || strings.Contains(rendered, "38;") {
		t.Fatalf("turn separator style = %q, want Codex-style dim modifier without custom foreground", rendered)
	}
}

func TestBlendRGBUsesBackgroundToForegroundDirection(t *testing.T) {
	background := terminalRGB{10, 20, 30}
	foreground := terminalRGB{210, 220, 230}
	if got := blendRGB(background, foreground, 0); got != background {
		t.Fatalf("alpha 0 = %#v", got)
	}
	if got := blendRGB(background, foreground, 1); got != foreground {
		t.Fatalf("alpha 1 = %#v", got)
	}
	if got := blendRGB(background, foreground, 0.5); got != (terminalRGB{110, 120, 130}) {
		t.Fatalf("alpha .5 = %#v", got)
	}
}
