package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type motionMode uint8

const (
	motionAnimated motionMode = iota
	motionReduced
)

type motionClock interface{ Now() time.Time }
type systemMotionClock struct{}

func (systemMotionClock) Now() time.Time { return time.Now() }

type fixedMotionClock struct{ now time.Time }

func (c fixedMotionClock) Now() time.Time { return c.now }

const (
	motionFrameInterval = 32 * time.Millisecond
	shimmerSweepPeriod  = 2 * time.Second
	shimmerPadding      = 10
	shimmerBandHalf     = 5.0
)

func shimmerText(text string, now, startedAt time.Time, mode motionMode, palette terminalPalette) string {
	if text == "" || mode == motionReduced || palette.NoColor {
		return text
	}
	runes := []rune(text)
	period := float64(len(runes) + shimmerPadding*2)
	elapsed := maxDuration(0, now.Sub(startedAt))
	position := math.Mod(elapsed.Seconds()/shimmerSweepPeriod.Seconds(), 1) * period
	var result strings.Builder
	for index, character := range runes {
		distance := math.Abs(float64(index+shimmerPadding) - position)
		intensity := 0.0
		if distance <= shimmerBandHalf {
			intensity = 0.5 * (1 + math.Cos(math.Pi*distance/shimmerBandHalf))
		}
		style := lipgloss.NewStyle().Bold(true)
		if palette.Level == colorLevelTrueColor {
			style = style.Foreground(palette.shimmerColor(intensity))
		} else if intensity < 0.2 {
			style = style.Bold(false).Faint(true)
		} else if intensity < 0.6 {
			style = style.Bold(false)
		}
		result.WriteString(style.Render(string(character)))
	}
	return result.String()
}

func activityIndicator(now, startedAt time.Time, mode motionMode, palette terminalPalette) string {
	if mode == motionReduced {
		return palette.dim().Render("•")
	}
	if palette.Level == colorLevelTrueColor && !palette.NoColor {
		return shimmerText("•", now, startedAt, mode, palette)
	}
	if (maxDuration(0, now.Sub(startedAt)).Milliseconds()/600)%2 == 0 {
		return "•"
	}
	return palette.dim().Render("◦")
}

func formatElapsedCompact(duration time.Duration) string {
	seconds := int64(maxDuration(0, duration).Round(time.Second) / time.Second)
	switch {
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dm %02ds", seconds/60, seconds%60)
	default:
		return fmt.Sprintf("%dh %02dm %02ds", seconds/3600, seconds%3600/60, seconds%60)
	}
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}
