package tui

import (
	"fmt"
	"math"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

type colorLevel uint8

const (
	colorLevelNone colorLevel = iota
	colorLevelANSI16
	colorLevelANSI256
	colorLevelTrueColor
)

type terminalRGB struct{ Red, Green, Blue uint8 }

type terminalPalette struct {
	Level      colorLevel
	Dark       bool
	NoColor    bool
	Foreground terminalRGB
	Background terminalRGB
}

func detectTerminalPalette(noColor bool) terminalPalette {
	if noColor {
		return terminalPalette{Level: colorLevelNone, NoColor: true, Dark: true, Foreground: terminalRGB{220, 220, 220}, Background: terminalRGB{20, 20, 20}}
	}
	level := colorLevelANSI16
	switch termenv.EnvColorProfile() {
	case termenv.TrueColor:
		level = colorLevelTrueColor
	case termenv.ANSI256:
		level = colorLevelANSI256
	case termenv.Ascii:
		level = colorLevelNone
	}
	dark := termenv.HasDarkBackground()
	foreground := terminalRGB{30, 30, 30}
	background := terminalRGB{245, 245, 245}
	if dark {
		foreground = terminalRGB{225, 225, 225}
		background = terminalRGB{18, 18, 18}
	}
	return terminalPalette{Level: level, Dark: dark, NoColor: level == colorLevelNone, Foreground: foreground, Background: background}
}

func (p terminalPalette) plain() lipgloss.Style { return lipgloss.NewStyle() }
func (p terminalPalette) bold() lipgloss.Style  { return p.plain().Bold(true) }
func (p terminalPalette) dim() lipgloss.Style {
	return p.plain().Faint(true).Foreground(p.semantic(terminalRGB{145, 145, 145}, "245", "242"))
}
func (p terminalPalette) accent() lipgloss.Style {
	return p.plain().Foreground(p.semantic(terminalRGB{74, 195, 214}, "81", "30"))
}
func (p terminalPalette) success() lipgloss.Style {
	return p.plain().Foreground(p.semantic(terminalRGB{72, 187, 120}, "78", "28"))
}
func (p terminalPalette) failure() lipgloss.Style {
	return p.plain().Foreground(p.semantic(terminalRGB{235, 107, 107}, "203", "160"))
}
func (p terminalPalette) warning() lipgloss.Style {
	return p.plain().Foreground(p.semantic(terminalRGB{224, 174, 72}, "221", "136"))
}
func (p terminalPalette) separator() lipgloss.Style { return p.dim() }
func (p terminalPalette) user() lipgloss.Style {
	if p.NoColor || p.Level != colorLevelTrueColor {
		return p.plain()
	}
	return p.plain().Background(p.semantic(blendRGB(p.Background, p.Foreground, 0.08), "236", "254"))
}

func (p terminalPalette) markdownAccent() string {
	if p.Dark {
		return "#4AC3D6"
	}
	return "#0E7490"
}

func (p terminalPalette) shimmerColor(intensity float64) lipgloss.TerminalColor {
	intensity = math.Max(0, math.Min(1, intensity))
	if p.NoColor || p.Level != colorLevelTrueColor {
		return p.semantic(p.Foreground, "255", "16")
	}
	return p.semantic(blendRGB(p.Foreground, p.Background, intensity*0.9), "255", "16")
}

func (p terminalPalette) semantic(rgb terminalRGB, darkFallback, lightFallback string) lipgloss.TerminalColor {
	if p.NoColor || p.Level == colorLevelNone {
		return lipgloss.NoColor{}
	}
	if p.Level == colorLevelTrueColor {
		return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", rgb.Red, rgb.Green, rgb.Blue))
	}
	if p.Dark {
		return lipgloss.Color(darkFallback)
	}
	return lipgloss.Color(lightFallback)
}

func blendRGB(background, foreground terminalRGB, alpha float64) terminalRGB {
	alpha = math.Max(0, math.Min(1, alpha))
	blend := func(base, overlay uint8) uint8 { return uint8(float64(base)*(1-alpha) + float64(overlay)*alpha) }
	return terminalRGB{blend(background.Red, foreground.Red), blend(background.Green, foreground.Green), blend(background.Blue, foreground.Blue)}
}
