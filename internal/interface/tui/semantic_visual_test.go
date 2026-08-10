package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestTerminalPaletteUsesCodexAccentByBackground(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })

	dark := terminalPalette{Level: colorLevelTrueColor, Dark: true, Foreground: terminalRGB{225, 225, 225}, Background: terminalRGB{18, 18, 18}}
	light := terminalPalette{Level: colorLevelTrueColor, Dark: false, Foreground: terminalRGB{30, 30, 30}, Background: terminalRGB{245, 245, 245}}

	if rendered := dark.selection().Render("selected"); !strings.Contains(rendered, "[1;36m") {
		t.Fatalf("dark selection does not use bold cyan: %q", rendered)
	}
	if rendered := light.selection().Render("selected"); !strings.Contains(rendered, "38;2;0;95;135") || !strings.Contains(rendered, "[1;") {
		t.Fatalf("light selection does not use bold deep cyan: %q", rendered)
	}
	if got := nearestANSI256(light.accentRGB()); got != 24 {
		t.Fatalf("light ANSI256 accent = %d, want 24", got)
	}
}

func TestComposerPromptAndStatusUseSemanticHierarchy(t *testing.T) {
	if fullscreenInputPrompt != "› " {
		t.Fatalf("composer prompt = %q, want %q", fullscreenInputPrompt, "› ")
	}
	if fullscreenInputPlaceholder != "Ask Amadeus to do anything, or type / for commands" {
		t.Fatalf("composer placeholder = %q", fullscreenInputPlaceholder)
	}

	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })

	_, model := newTestFullscreen(t, nil)
	model.palette = terminalPalette{Level: colorLevelTrueColor, Dark: true, Foreground: terminalRGB{225, 225, 225}, Background: terminalRGB{18, 18, 18}}
	if rendered := model.inputBox(); !strings.Contains(rendered, "\x1b[1m›") {
		t.Fatalf("composer prompt is not strong: %q", rendered)
	}
	model.input.Blur()
	if rendered := model.inputBox(); (!strings.Contains(rendered, "\x1b[2m") && !strings.Contains(rendered, "\x1b[2;")) || !strings.Contains(rendered, "›") {
		t.Fatalf("disabled composer prompt is not muted: %q", rendered)
	}

	normal := statusContextStyle(model.palette, 20).Render("context")
	if !strings.Contains(normal, "\x1b[38;2;18;192;18m") {
		t.Fatalf("normal context status is not green: %q", normal)
	}
	if strings.Contains(normal, "\x1b[2") {
		t.Fatalf("normal context status is unexpectedly dim: %q", normal)
	}
}

func TestInheritedNoColorDoesNotDisableRichTUIAccent(t *testing.T) {
	t.Setenv("TERM", "xterm")
	t.Setenv("COLORTERM", "")
	t.Setenv("NO_COLOR", "1")
	palette := detectTerminalPalette(false)
	if palette.NoColor || palette.Level != colorLevelANSI16 {
		t.Fatalf("palette unexpectedly disabled by inherited NO_COLOR: %#v", palette)
	}

	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })
	if rendered := palette.selection().Render("selected"); !strings.Contains(rendered, "\x1b[1;36m") {
		t.Fatalf("ANSI16 selection did not retain cyan: %q", rendered)
	}
}

func TestANSI16StatusBarUsesCodexAccentFamilies(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })

	_, model := newTestFullscreen(t, nil)
	model.palette = terminalPalette{Level: colorLevelANSI16, Dark: true}
	model.model = "gpt-top"
	model.startup.Project = "/workspace/amadeus"
	model.startup.Branch = "main"
	model.startup.ContextWindow = 128_000
	model.contextUsage = 32_000
	model.collaboration = CollaborationPlan
	rendered := model.statusBar()
	for _, sequence := range []string{"\x1b[36m", "\x1b[32m", "\x1b[35m"} {
		if !strings.Contains(rendered, sequence) {
			t.Fatalf("status bar omitted Codex color family %q: %q", sequence, rendered)
		}
	}
	plain := xansi.Strip(rendered)
	for _, expected := range []string{"gpt-top", "/workspace/amadeus", "main", "Plan", "Context 25% used", "128K window"} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("status bar omitted %q: %q", expected, plain)
		}
	}
}

func TestStatusLineAccentsMatchCodexFallbackAndSoftening(t *testing.T) {
	if ansi, _ := statusLineFallback(statusAccentMode); ansi != "6" {
		t.Fatalf("mode accent = %q, want cyan", ansi)
	}
	if ansi, _ := statusLineFallback(statusAccentUsage); ansi != "2" {
		t.Fatalf("usage accent = %q, want green", ansi)
	}
	if ansi, _ := statusLineFallback(statusAccentBranch); ansi != "5" {
		t.Fatalf("branch accent = %q, want magenta", ansi)
	}
	if got := softenStatusLineRGB(terminalRGB{0, 205, 205}); got != (terminalRGB{21, 196, 196}) {
		t.Fatalf("softened cyan = %#v", got)
	}
	if got := softenStatusLineRGB(terminalRGB{0, 205, 0}); got != (terminalRGB{18, 192, 18}) {
		t.Fatalf("softened green = %#v", got)
	}
	if got := softenStatusLineRGB(terminalRGB{205, 0, 205}); got != (terminalRGB{187, 13, 187}) {
		t.Fatalf("softened magenta = %#v", got)
	}
}

func TestToolHistoryUsesStrongTitlePlainCommandAndMutedOutput(t *testing.T) {
	ctx := HistoryRenderContext{Width: 80, Palette: terminalPalette{Level: colorLevelNone, NoColor: true}, Now: time.Now(), MotionStart: time.Now(), Motion: motionReduced}
	activity := &toolActivity{Title: "execute_command", Detail: "go test ./...", Result: "ok", Completed: true, Success: true}
	lines := renderExecLines(activity, ctx)
	if len(lines) != 2 {
		t.Fatalf("exec lines = %d, want 2", len(lines))
	}
	if lines[0][2].Text != "Ran" || lines[0][2].Style != styleBold {
		t.Fatalf("title span = %#v", lines[0][2])
	}
	if lines[0][3].Text != " go test ./..." || lines[0][3].Style != stylePlain {
		t.Fatalf("command span = %#v", lines[0][3])
	}
	if lines[1][1].Text != "ok" || lines[1][1].Style != styleDim {
		t.Fatalf("output span = %#v", lines[1][1])
	}

	explore := renderExploreLines([]*toolActivity{{Title: "Read docs/design.md", Completed: true, Success: true}}, ctx)
	if explore[1][1].Text != "Read" || explore[1][1].Style != styleAccent {
		t.Fatalf("explore verb span = %#v", explore[1][1])
	}
}

func TestMarkdownStyleMatchesVisualContract(t *testing.T) {
	palette := terminalPalette{Level: colorLevelTrueColor, Dark: true, Foreground: terminalRGB{225, 225, 225}, Background: terminalRGB{18, 18, 18}}
	renderer, err := newFullscreenMarkdownRenderer(100, palette)
	if err != nil {
		t.Fatal(err)
	}
	markdown := "# Heading\n\n`inline` and [link](https://example.com)\n\n> quote\n\n```go\npackage main\nfunc main() { println(\"hello\") }\n```\n\n```bash\necho \"hello\"\n```\n\n```json\n{\"value\": 1}\n```\n"
	rendered, err := renderer.Render(markdown)
	if err != nil {
		t.Fatal(err)
	}
	plain := xansi.Strip(rendered)
	for _, expected := range []string{"Heading", "inline", "link", "│ quote", "package main", "println", "echo", `{"value": 1}`} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("markdown output omitted %q: %q", expected, plain)
		}
	}
	if strings.Contains(rendered, "48;2;") || strings.Contains(rendered, "48;5;") {
		t.Fatalf("markdown unexpectedly uses a fixed background: %q", rendered)
	}
	if strings.Count(rendered, "\x1b[") < 6 {
		t.Fatalf("markdown lacks semantic/syntax highlighting: %q", rendered)
	}

	unknown, err := renderer.Render("```amadeus-unknown-language\nvalue = 1\n```\n")
	if err != nil {
		t.Fatalf("unknown language must degrade deterministically: %v", err)
	}
	if !strings.Contains(xansi.Strip(unknown), "value = 1") {
		t.Fatalf("unknown language content missing: %q", unknown)
	}

	lightPalette := terminalPalette{Level: colorLevelTrueColor, Dark: false, Foreground: terminalRGB{30, 30, 30}, Background: terminalRGB{245, 245, 245}}
	lightRenderer, err := newFullscreenMarkdownRenderer(100, lightPalette)
	if err != nil {
		t.Fatal(err)
	}
	lightCode, err := lightRenderer.Render("```go\npackage main\nvar value = 1\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	darkCode, err := renderer.Render("```go\npackage main\nvar value = 1\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	if lightCode == darkCode {
		t.Fatalf("light and dark code themes are identical: %q", lightCode)
	}
	if strings.Contains(lightCode, "48;2;") || strings.Contains(lightCode, "48;5;") {
		t.Fatalf("light code theme unexpectedly uses a background: %q", lightCode)
	}
}

func TestMarkdownNoColorHasNoANSI(t *testing.T) {
	renderer, err := newFullscreenMarkdownRenderer(80, terminalPalette{Level: colorLevelNone, NoColor: true, Dark: true})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := renderer.Render("**strong** and `code`\n\n```go\nvar value = 1\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, "\x1b[") {
		t.Fatalf("no-color markdown emitted ANSI: %q", rendered)
	}
}

func TestMarkdownANSI16DoesNotEmitHigherColorSequences(t *testing.T) {
	renderer, err := newFullscreenMarkdownRenderer(80, terminalPalette{Level: colorLevelANSI16, Dark: true})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := renderer.Render("`code`\n\n```go\nvar value = 1\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, "38;5;") || strings.Contains(rendered, "38;2;") || strings.Contains(rendered, "48;5;") || strings.Contains(rendered, "48;2;") {
		t.Fatalf("ANSI16 markdown emitted higher color sequence: %q", rendered)
	}
}

func TestSelectedListStylesWholeRow(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })

	_, model := newTestFullscreen(t, nil)
	model.palette = terminalPalette{Level: colorLevelTrueColor, Dark: true, Foreground: terminalRGB{225, 225, 225}, Background: terminalRGB{18, 18, 18}}
	rendered := model.renderListVisual(listVisual{Items: []listVisualItem{{Name: "/resume", Description: "resume a saved chat", Selected: true}}}, 80)
	accentPrefix := strings.Split(model.palette.selection().Render("X"), "X")[0]
	if count := strings.Count(rendered, accentPrefix); count < 3 {
		t.Fatalf("selected cursor/name/description do not share selection style: count=%d output=%q", count, rendered)
	}
}
