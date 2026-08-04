package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Generated from docs/Amadeus_logo.webp as terminal-safe monochrome Braille.
const wideAmadeusLogo = `
                            ⢀⣴⣾
                          ⣠⣶⡿⣿⣿
                       ⢀⣴⣾⠟⠁ ⣿⣿
                     ⣠⣾⡿⠋⢀⣤⡆ ⣿⣿
                  ⣀⣴⡿⠟⠁⣠⣴⡿⣿⡇ ⣿⣿
               ⢀⣠⣾⠿⠋⢀⣴⣾⠟⠉ ⣿⡇ ⣿⣿
             ⣀⣴⡿⠛⠁⣠⣶⡿⠋⠁   ⣿⡇ ⣿⣿
          ⢀⣤⣾⠟⠋⢀⣴⣾⣿⣿⣶⣶⣶⣶⣶⣶⣿⡇ ⣿⣿
        ⣠⣴⡿⠛⠁ ⠚⠛⠛⠋⠉⠉⠉⠉⠉⣉⣉⣉⣽⡇ ⠿⠿⠿⠿⠿⠿⠿⠿⣿⣿⣀⣀⣀⣀⣀⣀⣀
     ⢀⣴⣾⠟⠋⢀⡴⠒⠒⠒⠒⢶⡖⠒⠒⠒⠶⡾⠛⠛⠛⠁⡷⠒⠒⠒⠶⡖⣶⣶⣶ ⡿⠛⠛⠛⢛⣿⣿⠟⠁
   ⣠⣶⡿⠋⢁⣠⣾⣿⡇⣿⡇⢸⣿ ⡏⣩⣭⣭ ⡇⣿⣿⣿ ⡇⣙⣛⣛⣀⡇⣿⣿⣿ ⣧⣉⣉⣉⠹⣿⡇
⢀⣴⣾⣿⣭⣤⣤⣬⣭⣭⣭⣤⣿⣧⣼⣿ ⣧⣭⣭⣽⣤⣧⣬⣭⣭⣤⣷⣬⣭⣭⣭⣧⣬⣭⣭⣴⣯⣭⣭⣭⣴⣿⠇
⠈⠉⠉⠉⠉⠉⠉⠉⠉⠉⠉⠉⠉⠉⠉⢿⣶⣶⣶⣶⣶⣶⣶⣶⣶⣶⣶⡖  ⢀⣴⡿⠋⠉⠉⠉⠉⠉⠉⠉⠉
                          ⣿⡇⢀⣴⡿⠋
                          ⣿⣷⡿⠋
                          ⡿⠋`

const widePromptLogo = `

⠠⣤⣦
 ⠹⣿⣧
  ⠙⣿⣷⡀
   ⠘⣿⣷⡀
    ⠘⢿⣷⡄
     ⠈⢿⣿⡄
     ⢀⣼⣿⡟
    ⢠⣾⣿⠏
   ⣠⣿⡿⠃
  ⣴⣿⡿⠁
⢀⣾⣿⠟     ⢀⣀⣀⣀⣀⡀
 ⠈⠋      ⠸⠿⠿⠿⠿⠇`

const compactAmadeusLogo = `
             ⣠⣴⡇
          ⢀⣤⠞⣉⢸⡇
        ⣠⡶⢋⡴⠟⣿⢸⡇
     ⢀⣴⢞⣡⣾⣯⣤⣤⣿⢸⡇
   ⣠⠾⣋⡤⢭⣭⣤⣤⡤⠴⢯⣼⣿⣿⣿⢻⣷⣶⣶⠖
⣀⣴⣟⣁⣞⣃⣿⣿⢨⣿⣧⣜⣿⣧⣿⣷⣜⣛⣰⣿⣯⡿
       ⠘⠛⠛⠛⠛⠛⡿⢠⡾⠋
             ⡿⠋`

const compactPromptLogo = `

⢀⣄
⠹⣿⣆
 ⠙⣿⣧⡀
  ⠈⢿⣷⡀
  ⢀⣾⡿⠂
 ⣴⣿⠟⠁
⠺⡿⠃   ⣶⣶⣶`

const (
	wideLogoGap    = 8
	compactLogoGap = 4
)

var (
	wideLogo             = joinTerminalLogo(wideAmadeusLogo, widePromptLogo, wideLogoGap)
	compactLogo          = joinTerminalLogo(compactAmadeusLogo, compactPromptLogo, compactLogoGap)
	wideLogoMinimumWidth = terminalLogoWidth(wideLogo)
)

func terminalLogo(width int) string {
	if width >= wideLogoMinimumWidth {
		return wideLogo
	}
	return compactLogo
}

func joinTerminalLogo(brand, prompt string, gap int) string {
	brandLines := splitTerminalLogoLines(brand)
	promptLines := splitTerminalLogoLines(prompt)
	height := maxInt(len(brandLines), len(promptLines))
	brandWidth := 0
	for _, line := range brandLines {
		brandWidth = maxInt(brandWidth, lipgloss.Width(line))
	}
	lines := make([]string, height)
	for index := range height {
		brandLine := ""
		if index < len(brandLines) {
			brandLine = brandLines[index]
		}
		promptLine := ""
		if index < len(promptLines) {
			promptLine = promptLines[index]
		}
		if promptLine == "" {
			lines[index] = strings.TrimRight(brandLine, " ")
			continue
		}
		padding := maxInt(0, brandWidth-lipgloss.Width(brandLine)+gap)
		lines[index] = brandLine + strings.Repeat(" ", padding) + promptLine
	}
	return "\n" + strings.Join(lines, "\n")
}

func splitTerminalLogoLines(value string) []string {
	value = strings.TrimPrefix(value, "\n")
	value = strings.TrimSuffix(value, "\n")
	return strings.Split(value, "\n")
}

func terminalLogoWidth(value string) int {
	width := 0
	for _, line := range splitTerminalLogoLines(value) {
		width = maxInt(width, lipgloss.Width(line))
	}
	return width
}
