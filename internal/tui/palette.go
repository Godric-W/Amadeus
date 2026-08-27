package tui

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
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

type statusLineAccent uint8

const (
	statusAccentModel statusLineAccent = iota
	statusAccentPath
	statusAccentBranch
	statusAccentUsage
	statusAccentMode
	statusAccentThread
)

func detectTerminalPalette(noColor bool) terminalPalette {
	if noColor {
		return terminalPalette{Level: colorLevelNone, NoColor: true, Dark: true, Foreground: terminalRGB{220, 220, 220}, Background: terminalRGB{20, 20, 20}}
	}
	level := inferredTerminalColorLevel()
	if level == colorLevelNone {
		level = colorLevelANSI16
	}
	if profile := termenv.EnvColorProfile(); profile != termenv.Ascii {
		switch profile {
		case termenv.TrueColor:
			level = colorLevelTrueColor
		case termenv.ANSI256:
			level = colorLevelANSI256
		default:
			level = colorLevelANSI16
		}
	}
	dark := termenv.HasDarkBackground()
	foreground := terminalRGB{30, 30, 30}
	background := terminalRGB{245, 245, 245}
	if dark {
		foreground = terminalRGB{225, 225, 225}
		background = terminalRGB{18, 18, 18}
	}
	return terminalPalette{Level: level, Dark: dark, NoColor: false, Foreground: foreground, Background: background}
}

func inferredTerminalColorLevel() colorLevel {
	colorTerm := strings.ToLower(strings.TrimSpace(os.Getenv("COLORTERM")))
	if strings.Contains(colorTerm, "truecolor") || strings.Contains(colorTerm, "24bit") {
		return colorLevelTrueColor
	}
	term := strings.ToLower(strings.TrimSpace(os.Getenv("TERM")))
	if strings.Contains(term, "256color") {
		return colorLevelANSI256
	}
	if term == "" || term == "dumb" {
		return colorLevelNone
	}
	return colorLevelANSI16
}

func (p terminalPalette) plain() lipgloss.Style  { return lipgloss.NewStyle() }
func (p terminalPalette) strong() lipgloss.Style { return p.plain().Bold(true) }
func (p terminalPalette) bold() lipgloss.Style   { return p.strong() }
func (p terminalPalette) muted() lipgloss.Style {
	style := p.plain().Faint(true)
	if p.Level == colorLevelTrueColor || p.Level == colorLevelANSI256 {
		style = style.Foreground(p.bestColor(blendRGB(p.Background, p.Foreground, 0.55)))
	}
	return style
}
func (p terminalPalette) dim() lipgloss.Style { return p.muted() }
func (p terminalPalette) accent() lipgloss.Style {
	if p.NoColor || p.Level == colorLevelNone {
		return p.strong()
	}
	if p.Dark || p.Level == colorLevelANSI16 {
		return p.plain().Foreground(lipgloss.Color("6")).Bold(true)
	}
	return p.plain().Foreground(p.bestColor(p.accentRGB())).Bold(true)
}
func (p terminalPalette) command() lipgloss.Style {
	if p.NoColor || p.Level == colorLevelNone {
		return p.strong()
	}
	if p.Level == colorLevelANSI16 {
		return p.plain().Foreground(lipgloss.Color("5"))
	}
	return p.plain().Foreground(p.bestColor(terminalRGB{205, 0, 205}))
}
func (p terminalPalette) selection() lipgloss.Style { return p.accent().Bold(true) }
func (p terminalPalette) success() lipgloss.Style {
	return p.statusColor(terminalRGB{72, 187, 120}, "2", "2").Bold(true)
}
func (p terminalPalette) failure() lipgloss.Style {
	return p.statusColor(terminalRGB{235, 107, 107}, "1", "1").Bold(true)
}
func (p terminalPalette) warning() lipgloss.Style {
	return p.statusColor(terminalRGB{224, 174, 72}, "3", "3")
}
func (p terminalPalette) statusLineStyle(accent statusLineAccent) lipgloss.Style {
	if p.NoColor || p.Level == colorLevelNone {
		return p.muted()
	}
	ansi, rgb := statusLineFallback(accent)
	if p.Level == colorLevelANSI16 {
		return p.plain().Foreground(lipgloss.Color(ansi))
	}
	if themed, ok := statusLineThemeRGB(accent, p.Dark); ok {
		rgb = themed
	}
	return p.plain().Foreground(p.bestColor(softenStatusLineRGB(rgb)))
}

func statusLineThemeRGB(accent statusLineAccent, dark bool) (terminalRGB, bool) {
	themeName := "catppuccin-mocha"
	if !dark {
		themeName = "catppuccin-latte"
	}
	token := chroma.NameClass
	switch accent {
	case statusAccentPath:
		token = chroma.LiteralString
	case statusAccentBranch:
		token = chroma.NameFunction
	case statusAccentUsage:
		token = chroma.LiteralNumber
	case statusAccentMode:
		token = chroma.Keyword
	case statusAccentThread:
		token = chroma.GenericHeading
	}
	colour := chromastyles.Get(themeName).Get(token).Colour
	if !colour.IsSet() {
		return terminalRGB{}, false
	}
	return terminalRGB{Red: colour.Red(), Green: colour.Green(), Blue: colour.Blue()}, true
}

func statusLineFallback(accent statusLineAccent) (string, terminalRGB) {
	switch accent {
	case statusAccentPath, statusAccentUsage:
		return "2", terminalRGB{0, 205, 0}
	case statusAccentBranch, statusAccentMode, statusAccentThread:
		return "5", terminalRGB{205, 0, 205}
	default:
		return "6", terminalRGB{0, 205, 205}
	}
}

func softenStatusLineRGB(color terminalRGB) terminalRGB {
	luma := (77*uint16(color.Red) + 150*uint16(color.Green) + 29*uint16(color.Blue)) / 256
	soften := func(channel uint8) uint8 {
		return uint8((uint16(channel)*85 + luma*15 + 50) / 100)
	}
	return terminalRGB{Red: soften(color.Red), Green: soften(color.Green), Blue: soften(color.Blue)}
}

func (p terminalPalette) separator() lipgloss.Style {
	if p.Level == colorLevelTrueColor || p.Level == colorLevelANSI256 {
		return p.plain().Foreground(p.bestColor(blendRGB(p.Background, p.Foreground, 0.20)))
	}
	return p.muted()
}
func (p terminalPalette) turnSeparator() lipgloss.Style {
	if p.NoColor || p.Level == colorLevelNone {
		return p.plain()
	}
	return p.plain().Faint(true)
}
func (p terminalPalette) border() lipgloss.Style {
	if p.Level == colorLevelTrueColor || p.Level == colorLevelANSI256 {
		return p.plain().Foreground(p.bestColor(blendRGB(p.Background, p.Foreground, 0.32)))
	}
	return p.muted()
}
func (p terminalPalette) user() lipgloss.Style {
	if p.NoColor || p.Level != colorLevelTrueColor {
		return p.plain()
	}
	alpha := 0.04
	if p.Dark {
		alpha = 0.12
	}
	return p.plain().Background(p.bestColor(blendRGB(p.Background, p.Foreground, alpha)))
}

func (p terminalPalette) accentRGB() terminalRGB {
	if p.Dark {
		return terminalRGB{0, 205, 205}
	}
	return terminalRGB{0, 95, 135}
}

func (p terminalPalette) markdownAccent() string {
	if p.NoColor || p.Level == colorLevelNone {
		return ""
	}
	if p.Dark || p.Level == colorLevelANSI16 {
		return "6"
	}
	return p.markdownColor(p.accentRGB())
}

func (p terminalPalette) markdownColor(rgb terminalRGB) string {
	if p.NoColor || p.Level == colorLevelNone {
		return ""
	}
	if p.Level == colorLevelANSI16 {
		return fmt.Sprintf("%d", nearestANSI16(rgb))
	}
	if p.Level == colorLevelANSI256 {
		return fmt.Sprintf("%d", nearestANSI256(rgb))
	}
	return fmt.Sprintf("#%02X%02X%02X", rgb.Red, rgb.Green, rgb.Blue)
}

func (p terminalPalette) colorProfile() termenv.Profile {
	switch p.Level {
	case colorLevelTrueColor:
		return termenv.TrueColor
	case colorLevelANSI256:
		return termenv.ANSI256
	case colorLevelANSI16:
		return termenv.ANSI
	default:
		return termenv.Ascii
	}
}

func (p terminalPalette) chromaFormatter() string {
	switch p.Level {
	case colorLevelTrueColor:
		return "terminal16m"
	case colorLevelANSI256:
		return "terminal256"
	case colorLevelANSI16:
		return "terminal16"
	default:
		return "terminal"
	}
}

func (p terminalPalette) shimmerColor(intensity float64) lipgloss.TerminalColor {
	intensity = math.Max(0, math.Min(1, intensity))
	if p.NoColor || p.Level != colorLevelTrueColor {
		return p.bestColor(p.Foreground)
	}
	return p.bestColor(blendRGB(p.Foreground, p.Background, intensity*0.9))
}

func (p terminalPalette) statusColor(rgb terminalRGB, darkFallback, lightFallback string) lipgloss.Style {
	if p.NoColor || p.Level == colorLevelNone {
		return p.plain()
	}
	if p.Level == colorLevelANSI16 {
		if p.Dark {
			return p.plain().Foreground(lipgloss.Color(darkFallback))
		}
		return p.plain().Foreground(lipgloss.Color(lightFallback))
	}
	return p.plain().Foreground(p.bestColor(rgb))
}

func (p terminalPalette) bestColor(rgb terminalRGB) lipgloss.TerminalColor {
	if p.NoColor || p.Level == colorLevelNone || p.Level == colorLevelANSI16 {
		return lipgloss.NoColor{}
	}
	if p.Level == colorLevelANSI256 {
		return lipgloss.Color(fmt.Sprintf("%d", nearestANSI256(rgb)))
	}
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", rgb.Red, rgb.Green, rgb.Blue))
}

func nearestANSI16(target terminalRGB) uint8 {
	colors := [...]terminalRGB{
		{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0},
		{0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
	}
	bestIndex := uint8(0)
	bestDistance := math.MaxFloat64
	for index, candidate := range colors {
		red := float64(int(target.Red) - int(candidate.Red))
		green := float64(int(target.Green) - int(candidate.Green))
		blue := float64(int(target.Blue) - int(candidate.Blue))
		distance := red*red*0.299 + green*green*0.587 + blue*blue*0.114
		if distance < bestDistance {
			bestDistance = distance
			bestIndex = uint8(index)
		}
	}
	return bestIndex
}

func nearestANSI256(target terminalRGB) uint8 {
	bestIndex := uint8(16)
	bestDistance := math.MaxFloat64
	for index := 16; index <= 255; index++ {
		candidate := ansi256RGB(uint8(index))
		red := float64(int(target.Red) - int(candidate.Red))
		green := float64(int(target.Green) - int(candidate.Green))
		blue := float64(int(target.Blue) - int(candidate.Blue))
		distance := red*red*0.299 + green*green*0.587 + blue*blue*0.114
		if distance < bestDistance {
			bestDistance = distance
			bestIndex = uint8(index)
		}
	}
	return bestIndex
}

func ansi256RGB(index uint8) terminalRGB {
	if index >= 232 {
		value := uint8(8 + 10*(int(index)-232))
		return terminalRGB{value, value, value}
	}
	cube := int(index) - 16
	levels := [...]uint8{0, 95, 135, 175, 215, 255}
	return terminalRGB{levels[cube/36], levels[(cube/6)%6], levels[cube%6]}
}

func blendRGB(background, foreground terminalRGB, alpha float64) terminalRGB {
	alpha = math.Max(0, math.Min(1, alpha))
	blend := func(base, overlay uint8) uint8 { return uint8(float64(base)*(1-alpha) + float64(overlay)*alpha) }
	return terminalRGB{blend(background.Red, foreground.Red), blend(background.Green, foreground.Green), blend(background.Blue, foreground.Blue)}
}
