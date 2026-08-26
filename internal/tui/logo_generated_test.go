package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestTerminalLogoUsesResponsiveAssets(t *testing.T) {
	if got := terminalLogo(80); !strings.Contains(got, "⣿") || !strings.Contains(got, "⠸⠿") || strings.Contains(got, "Amadeus >_") {
		t.Fatalf("wide logo is incomplete: %q", got)
	}
	assertPromptDoesNotOverlap(t, wideAmadeusLogo, widePromptLogo, wideLogo, wideLogoGap)
	if got := terminalLogo(60); got != compactLogo || !strings.Contains(got, "⣿") || !strings.Contains(got, "⣶⣶⣶") {
		t.Fatalf("compact logo = %q", got)
	}
	assertPromptDoesNotOverlap(t, compactAmadeusLogo, compactPromptLogo, compactLogo, compactLogoGap)
	for _, line := range splitTerminalLogoLines(compactLogo) {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("compact logo exceeds 40 columns: %q", line)
		}
	}
}

func assertPromptDoesNotOverlap(t *testing.T, brand, prompt, combined string, gap int) {
	t.Helper()
	brandWidth := 0
	for _, line := range splitTerminalLogoLines(brand) {
		brandWidth = maxInt(brandWidth, lipgloss.Width(line))
	}
	promptLines := splitTerminalLogoLines(prompt)
	combinedLines := splitTerminalLogoLines(combined)
	foundPrompt := false
	for index, promptLine := range promptLines {
		if promptLine == "" {
			continue
		}
		foundPrompt = true
		if index >= len(combinedLines) || !strings.HasSuffix(combinedLines[index], promptLine) {
			t.Fatalf("combined logo line %d omitted prompt %q", index, promptLine)
		}
		column := lipgloss.Width(combinedLines[index]) - lipgloss.Width(promptLine)
		if column < brandWidth+gap {
			t.Fatalf("prompt starts at column %d before fixed boundary %d: %q", column, brandWidth+gap, combinedLines[index])
		}
	}
	if !foundPrompt {
		t.Fatal("combined logo has no pixel prompt")
	}
}

func TestTerminalLogoUsesCompactAssetBelowWideBreakpoint(t *testing.T) {
	for _, width := range []int{40, wideLogoMinimumWidth - 1} {
		if got := terminalLogo(width); got != compactLogo {
			t.Fatalf("terminalLogo(%d) = %q", width, got)
		}
	}
	for _, width := range []int{wideLogoMinimumWidth, 80, 100, 160} {
		if got := terminalLogo(width); got != wideLogo {
			t.Fatalf("terminalLogo(%d) did not use wide asset", width)
		}
		for _, line := range strings.Split(strings.Trim(wideLogo, "\n"), "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("wide logo line exceeds minimum width %d: %q", width, line)
			}
		}
	}
}
