package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestShimmerUsesFixedClockAndTwoSecondSweep(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })
	palette := terminalPalette{Level: colorLevelTrueColor, Dark: true, Foreground: terminalRGB{230, 230, 230}, Background: terminalRGB{20, 20, 20}}
	start := time.Unix(100, 0)
	first := shimmerText("Working", start, start, motionAnimated, palette)
	second := shimmerText("Working", start.Add(500*time.Millisecond), start, motionAnimated, palette)
	repeated := shimmerText("Working", start.Add(2*time.Second), start, motionAnimated, palette)
	if first == second {
		t.Fatal("shimmer frame did not move")
	}
	if first != repeated {
		t.Fatal("shimmer did not repeat after two seconds")
	}
	if !strings.Contains(first, "\x1b[") {
		t.Fatalf("true-color shimmer has no ANSI: %q", first)
	}
}

func TestActivityIndicatorANSIAndReducedMotion(t *testing.T) {
	palette := terminalPalette{Level: colorLevelANSI16, Dark: true}
	start := time.Unix(100, 0)
	if got := activityIndicator(start, start, motionAnimated, palette); got != "•" {
		t.Fatalf("initial marker = %q", got)
	}
	if got := activityIndicator(start.Add(600*time.Millisecond), start, motionAnimated, palette); !strings.Contains(got, "◦") {
		t.Fatalf("fallback marker = %q", got)
	}
	if got := activityIndicator(start.Add(time.Second), start, motionReduced, palette); !strings.Contains(got, "•") {
		t.Fatalf("reduced marker = %q", got)
	}
}

func TestSpinnerGlyphUsesClaudeFramesAndPlainForeground(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	palette := terminalPalette{Level: colorLevelTrueColor, Dark: true, Foreground: terminalRGB{230, 230, 230}, Background: terminalRGB{20, 20, 20}}
	start := time.Unix(100, 0)
	want := []string{"· ", "✢ ", "✱ ", "✶ ", "✻ ", "✽ ", "✽ ", "✻ ", "✶ ", "✱ ", "✢ ", "· ", "· "}
	for index, expected := range want {
		got := spinnerGlyph(start.Add(time.Duration(index)*spinnerFramePeriod), start, motionAnimated, palette)
		if got != expected {
			t.Fatalf("spinner frame %d = %q, want %q", index, got, expected)
		}
		if strings.Contains(got, "\x1b[") {
			t.Fatalf("spinner frame uses a themed color: %q", got)
		}
		if width := lipgloss.Width(got); width != 2 {
			t.Fatalf("spinner frame width = %d, want 2: %q", width, got)
		}
	}
	if got := spinnerGlyph(start.Add(time.Second), start, motionReduced, palette); got != "✻ " {
		t.Fatalf("reduced-motion spinner = %q", got)
	}
}

func TestSpinnerGlyphUsesGhosttySafeFinalFrame(t *testing.T) {
	t.Setenv("TERM", "xterm-ghostty")
	palette := terminalPalette{NoColor: true}
	start := time.Unix(100, 0)
	if got := spinnerGlyph(start.Add(5*spinnerFramePeriod), start, motionAnimated, palette); got != "* " {
		t.Fatalf("Ghostty final spinner frame = %q, want %q", got, "* ")
	}
}

func TestFormatElapsedCompactZeroPadsClockUnits(t *testing.T) {
	for duration, expected := range map[time.Duration]string{
		0: "0s", 59 * time.Second: "59s", 60 * time.Second: "1m 00s", 185 * time.Second: "3m 05s", 3661 * time.Second: "1h 01m 01s",
	} {
		if got := formatElapsedCompact(duration); got != expected {
			t.Fatalf("%s = %q, want %q", duration, got, expected)
		}
	}
}
