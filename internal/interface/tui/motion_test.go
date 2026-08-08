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

func TestFormatElapsedCompactZeroPadsClockUnits(t *testing.T) {
	for duration, expected := range map[time.Duration]string{
		0: "0s", 59 * time.Second: "59s", 60 * time.Second: "1m 00s", 185 * time.Second: "3m 05s", 3661 * time.Second: "1h 01m 01s",
	} {
		if got := formatElapsedCompact(duration); got != expected {
			t.Fatalf("%s = %q, want %q", duration, got, expected)
		}
	}
}
