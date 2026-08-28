package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
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
	if inputPrompt != "› " {
		t.Fatalf("composer prompt = %q, want %q", inputPrompt, "› ")
	}
	if inputPlaceholder != "Ask Amadeus to do anything" {
		t.Fatalf("composer placeholder = %q", inputPlaceholder)
	}

	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })

	_, model := newTestModel(t, nil)
	model.palette = terminalPalette{Level: colorLevelTrueColor, Dark: true, Foreground: terminalRGB{225, 225, 225}, Background: terminalRGB{18, 18, 18}}
	if rendered := model.inputBox(); !strings.Contains(rendered, "\x1b[1m›") {
		t.Fatalf("composer prompt is not strong: %q", rendered)
	}
	model.input.Blur()
	if rendered := model.inputBox(); (!strings.Contains(rendered, "\x1b[2m") && !strings.Contains(rendered, "\x1b[2;")) || !strings.Contains(rendered, "›") {
		t.Fatalf("disabled composer prompt is not muted: %q", rendered)
	}

	normal := statusContextStyle(model.palette, 20).Render("context")
	if !strings.Contains(normal, "\x1b[38;2;242;181;144m") {
		t.Fatalf("normal context status does not use the Codex usage theme color: %q", normal)
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

	_, model := newTestModel(t, nil)
	model.palette = terminalPalette{Level: colorLevelANSI16, Dark: true}
	model.session.Configuration.Model = "gpt-top"
	model.session.Configuration.CWD = "/workspace/amadeus"
	model.session.Configuration.Mode = protocol.ModeKindPlan
	model.session.ContextWindow = 128_000
	model.session.ContextUsed = 32_000
	model.workspace = statusLineWorkspaceState{Generation: model.session.Generation, CurrentDir: "/workspace/amadeus", Branch: "main"}
	model.refreshStatusLine()
	rendered := model.footerView()
	for _, sequence := range []string{"\x1b[36m", "\x1b[32m", "\x1b[35m"} {
		if !strings.Contains(rendered, sequence) {
			t.Fatalf("status bar omitted Codex color family %q: %q", sequence, rendered)
		}
	}
	plain := xansi.Strip(rendered)
	for _, expected := range []string{"gpt-top", "amadeus", "main", "Plan mode", "Context 25% used", "128K window"} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("status bar omitted %q: %q", expected, plain)
		}
	}
	if !strings.HasSuffix(plain, "Plan mode (shift+tab to cycle)  ") || lipgloss.Width(plain) != model.width {
		t.Fatalf("Plan mode is not right-aligned with Codex padding: %q", plain)
	}
}

func TestNarrowStatusLineRetainsGitBranchWithPlanIndicator(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.width = 40
	model.session.Configuration.Model = "gpt-top"
	model.session.Configuration.CWD = "/workspace/team/amadeus"
	model.session.Configuration.Mode = protocol.ModeKindPlan
	model.session.Title = "long session title"
	model.session.ContextWindow = 128_000
	model.session.ContextUsed = 32_000
	model.workspace = statusLineWorkspaceState{Generation: model.session.Generation, CurrentDir: "/workspace/team/amadeus", Branch: "feature/status-line"}
	model.refreshStatusLine()

	plain := xansi.Strip(model.footerView())
	if !strings.Contains(plain, "feature/status-line") {
		t.Fatalf("narrow status line lost git branch: %q", plain)
	}
	if !strings.HasSuffix(plain, "Plan mode  ") || lipgloss.Width(plain) != model.width {
		t.Fatalf("narrow status line did not retain right-aligned mode: %q", plain)
	}
	if lipgloss.Width(plain) > model.width {
		t.Fatalf("narrow status bar width = %d, want <= %d: %q", lipgloss.Width(plain), model.width, plain)
	}
}

func TestFooterReservesIndependentRightModeColumn(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.session.Configuration.Model = "gpt-top"
	model.session.Configuration.CWD = "/workspace/team/amadeus-with-a-long-directory-name"
	model.session.Configuration.Mode = protocol.ModeKindPlan
	model.session.Title = "a long thread title that competes for footer width"
	model.session.ContextWindow = 128_000
	model.session.ContextUsed = 96_000
	model.workspace = statusLineWorkspaceState{
		Generation: model.session.Generation,
		CurrentDir: model.session.Configuration.CWD,
		Branch:     "feature/long-footer-layout",
	}
	model.refreshStatusLine()

	for _, width := range []int{40, 60, 80, 100, 120} {
		model.width = width
		plain := xansi.Strip(model.footerView())
		if got := lipgloss.Width(plain); got > width {
			t.Fatalf("footer width at %d columns = %d: %q", width, got, plain)
		}
		if !strings.HasSuffix(plain, "  ") {
			t.Fatalf("footer at %d columns lost right padding: %q", width, plain)
		}
		label := "Plan mode"
		if strings.Contains(plain, "Plan mode (shift+tab to cycle)") {
			label = "Plan mode (shift+tab to cycle)"
		}
		if start := strings.LastIndex(plain, label); start < 0 || lipgloss.Width(plain[:start])+lipgloss.Width(label)+footerRightPadding != width {
			t.Fatalf("mode label is not independently right-aligned at %d columns: %q", width, plain)
		}
	}
}

func TestStatusLineAccentsMatchCodexFallbackAndSoftening(t *testing.T) {
	if ansi, _ := statusLineFallback(statusAccentMode); ansi != "5" {
		t.Fatalf("mode accent = %q, want magenta", ansi)
	}
	if ansi, _ := statusLineFallback(statusAccentUsage); ansi != "2" {
		t.Fatalf("usage accent = %q, want green", ansi)
	}
	if ansi, _ := statusLineFallback(statusAccentBranch); ansi != "5" {
		t.Fatalf("branch accent = %q, want magenta", ansi)
	}
	for _, test := range []struct {
		name   string
		accent statusLineAccent
		dark   bool
		want   terminalRGB
	}{
		{name: "dark model", accent: statusAccentModel, dark: true, want: terminalRGB{249, 226, 175}},
		{name: "dark path", accent: statusAccentPath, dark: true, want: terminalRGB{166, 227, 161}},
		{name: "dark branch", accent: statusAccentBranch, dark: true, want: terminalRGB{137, 180, 250}},
		{name: "dark usage", accent: statusAccentUsage, dark: true, want: terminalRGB{250, 179, 135}},
		{name: "dark mode", accent: statusAccentMode, dark: true, want: terminalRGB{203, 166, 247}},
		{name: "light model", accent: statusAccentModel, dark: false, want: terminalRGB{223, 142, 29}},
		{name: "light path", accent: statusAccentPath, dark: false, want: terminalRGB{64, 160, 43}},
		{name: "light branch", accent: statusAccentBranch, dark: false, want: terminalRGB{30, 102, 245}},
		{name: "light usage", accent: statusAccentUsage, dark: false, want: terminalRGB{254, 100, 11}},
		{name: "light mode", accent: statusAccentMode, dark: false, want: terminalRGB{136, 57, 239}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got, ok := statusLineThemeRGB(test.accent, test.dark); !ok || got != test.want {
				t.Fatalf("theme color = %#v available=%v, want %#v", got, ok, test.want)
			}
		})
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

func TestTrueColorStatusLineUsesAdaptiveCodexThemeColors(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })

	_, model := newTestModel(t, nil)
	model.palette = terminalPalette{Level: colorLevelTrueColor, Dark: true}
	model.session.Configuration.Model = "gpt-top"
	model.session.Configuration.CWD = "/workspace/amadeus"
	model.workspace = statusLineWorkspaceState{Generation: model.session.Generation, CurrentDir: "/workspace/amadeus", Branch: "main"}
	model.session.ContextWindow = 128_000
	model.refreshStatusLine()

	rendered := model.footerView()
	for _, sequence := range []string{"38;2;246;226;183", "38;2;171;223;167", "38;2;143;179;239", "38;2;242;181;144"} {
		if !strings.Contains(rendered, sequence) {
			t.Fatalf("statusline omitted softened Codex theme color %q: %q", sequence, rendered)
		}
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
	markdown := "# Heading\n\n`inline` and [link](https://example.com)\n\n> quote\n\n```go\npackage main\nfunc main() { println(\"hello\") }\n```\n\n```bash\necho \"hello\"\n```\n\n```json\n{\"value\": 1}\n```\n"
	lines := newMarkdownRenderer().Render(newMarkdownSource(markdown, "/workspace"), HistoryRenderRich)
	plain := strings.Join(rawStyledLines(styledLinesFromMarkdown(lines)), "\n")
	for _, expected := range []string{"Heading", "inline", "link", "│ quote", "package main", "println", "echo", `{"value": 1}`} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("markdown output omitted %q: %q", expected, plain)
		}
	}
}

func TestMarkdownHeadingQuoteAndListMarkerSemantics(t *testing.T) {
	renderer := newMarkdownRenderer()
	headings := renderer.Render(newMarkdownSource("# H1\n\n### H3\n", ""), HistoryRenderRich)
	if len(headings) < 3 || len(headings[0].Spans) == 0 || !headings[0].Spans[0].Markdown.Bold || !headings[0].Spans[0].Markdown.Underline {
		t.Fatalf("H1 semantics = %#v", headings)
	}
	if len(headings[2].Spans) == 0 || !headings[2].Spans[0].Markdown.Bold || !headings[2].Spans[0].Markdown.Italic {
		t.Fatalf("H3 semantics = %#v", headings)
	}
	quote := renderer.Render(newMarkdownSource("> quoted\n", ""), HistoryRenderRich)
	if len(quote) != 1 || len(quote[0].InitialIndent) == 0 || quote[0].InitialIndent[0].Style != styleQuote || quote[0].Spans[0].Style != styleQuote {
		t.Fatalf("quote semantics = %#v", quote)
	}
	ordered := renderer.Render(newMarkdownSource("1. first\n", ""), HistoryRenderRich)
	if len(ordered) != 1 || len(ordered[0].InitialIndent) == 0 || ordered[0].InitialIndent[0].Style != styleOrderedListMarker {
		t.Fatalf("ordered marker semantics = %#v", ordered)
	}
}

func TestMarkdownNoColorHasNoANSI(t *testing.T) {
	rendered := renderStyledLines(styledLinesFromMarkdown(newMarkdownRenderer().Render(newMarkdownSource("**strong** and `code`\n\n```go\nvar value = 1\n```\n", ""), HistoryRenderRich)), noColorRenderContext())
	if strings.Contains(rendered, "\x1b[") {
		t.Fatalf("no-color markdown emitted ANSI: %q", rendered)
	}
}

func TestMarkdownANSI16DoesNotEmitHigherColorSequences(t *testing.T) {
	ctx := noColorRenderContext()
	ctx.Palette = terminalPalette{Level: colorLevelANSI16, Dark: true}
	rendered := renderStyledLines(styledLinesFromMarkdown(newMarkdownRenderer().Render(newMarkdownSource("`code`\n\n```go\nvar value = 1\n```\n", ""), HistoryRenderRich)), ctx)
	if strings.Contains(rendered, "38;5;") || strings.Contains(rendered, "38;2;") || strings.Contains(rendered, "48;5;") || strings.Contains(rendered, "48;2;") {
		t.Fatalf("ANSI16 markdown emitted higher color sequence: %q", rendered)
	}
}

func TestMarkdownCodeUsesChromaTokenStyles(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })
	ctx := noColorRenderContext()
	ctx.Palette = terminalPalette{Level: colorLevelTrueColor, Dark: true}
	lines := styledLinesFromMarkdown(newMarkdownRenderer().Render(newMarkdownSource("```go\nfunc main() {}\n```\n", ""), HistoryRenderRich))
	rendered := renderStyledLines(lines, ctx)
	if !strings.Contains(rendered, "\x1b[") {
		t.Fatalf("Chroma code rendering has no terminal style: %q", rendered)
	}
}

func TestMarkdownStrongRendersBoldInRichColorSurface(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })
	ctx := HistoryRenderContext{Width: 80, Palette: terminalPalette{Level: colorLevelTrueColor, Dark: true}}
	cell := NewAgentMarkdownCell(newMarkdownSource("**Interface**", ""))
	rendered := renderStyledLines(cell.DisplayLines(ctx), ctx)
	if !strings.Contains(rendered, "\x1b[1m") && !strings.Contains(rendered, "\x1b[1;") {
		t.Fatalf("strong markdown did not render bold: %q", rendered)
	}
}

func TestMarkdownRenderProjectsOSC8OnlyWhenEnabled(t *testing.T) {
	ctx := noColorRenderContext()
	ctx.Hyperlinks = true
	line := styledLine{{Text: "docs", Style: styleAccent, Destination: "https://example.com/docs"}}
	if rendered := renderStyledLines([]styledLine{line}, ctx); !strings.Contains(rendered, "\x1b]8;;https://example.com/docs\x1b\\") {
		t.Fatalf("OSC-8 projection missing: %q", rendered)
	}
	ctx.Hyperlinks = false
	if rendered := renderStyledLines([]styledLine{line}, ctx); strings.Contains(rendered, "\x1b]8;;") {
		t.Fatalf("OSC-8 projection ignored capability: %q", rendered)
	}
}

func TestMarkdownTerminalProjectionStripsSourceControlSequences(t *testing.T) {
	source := "before \x1b[31mred\x1b[0m \x1b]8;;https://evil.example\x1b\\link\x1b]8;;\x1b\\ after"
	lines := newMarkdownRenderer().Render(newMarkdownSource(source, ""), HistoryRenderRich)
	rendered := renderStyledLines(styledLinesFromMarkdown(lines), noColorRenderContext())
	if strings.Contains(rendered, "\x1b") || !strings.Contains(rendered, "before red link after") {
		t.Fatalf("unsafe markdown terminal projection = %q", rendered)
	}
}

func TestMarkdownRichLayoutKeepsReadableBlockSpacingAndWords(t *testing.T) {
	ctx := noColorRenderContext()
	ctx.Width = 24
	cell := NewAgentMarkdownCell(newMarkdownSource("# Interface\n\n**Interface** provides a stable contract.\n\n- first item\n- second item\n", ""))
	rendered := xansi.Strip(renderStyledLines(cell.DisplayLines(ctx), ctx))
	for _, expected := range []string{"• # Interface", "Interface provides", "• first item", "• second item"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("rich layout omitted %q: %q", expected, rendered)
		}
	}
	if strings.Contains(rendered, "Inter\n  face") {
		t.Fatalf("ordinary word split across lines: %q", rendered)
	}
	if strings.Count(rendered, "\n  \n") < 2 {
		t.Fatalf("block spacing is missing: %q", rendered)
	}
}

func TestSelectedListStylesWholeRow(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })

	_, model := newTestModel(t, nil)
	model.palette = terminalPalette{Level: colorLevelTrueColor, Dark: true, Foreground: terminalRGB{225, 225, 225}, Background: terminalRGB{18, 18, 18}}
	rendered := model.renderListVisual(listVisual{Items: []listVisualItem{{Name: "/resume", Description: "resume a saved chat", Selected: true}}}, 80)
	accentPrefix := strings.Split(model.palette.selection().Render("X"), "X")[0]
	if count := strings.Count(rendered, accentPrefix); count < 3 {
		t.Fatalf("selected cursor/name/description do not share selection style: count=%d output=%q", count, rendered)
	}
}
