package tui

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	defaultIdleScreenWidth = 80
	minimumIdleScreenWidth = 48
)

func WriteIdleScreen(output io.Writer, projectRoot string, width int) error {
	if output == nil {
		return errors.New("inline idle screen output is nil")
	}
	if width < minimumIdleScreenWidth {
		width = defaultIdleScreenWidth
	}
	innerWidth := width - 2
	lines := []string{
		"Amadeus · Ready",
		"Project: " + inlineIdleText(projectRoot),
		"Describe a task below, or type / to list commands.",
		"/resume  /plan  /skills  /status  /mcp  /clear",
	}
	if _, err := fmt.Fprintf(output, "╭%s╮\n", strings.Repeat("─", innerWidth)); err != nil {
		return fmt.Errorf("write inline idle screen header: %w", err)
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(output, "│%s│\n", inlineIdleLine(line, innerWidth)); err != nil {
			return fmt.Errorf("write inline idle screen body: %w", err)
		}
	}
	if _, err := fmt.Fprintf(output, "╰%s╯\n", strings.Repeat("─", innerWidth)); err != nil {
		return fmt.Errorf("write inline idle screen footer: %w", err)
	}
	_, err := fmt.Fprintln(output, "status: phase=idle task=- tools=0 usage=0/0")
	if err != nil {
		return fmt.Errorf("write inline idle screen status: %w", err)
	}
	return nil
}

func inlineIdleLine(content string, width int) string {
	content = inlineIdleText(content)
	runes := []rune(content)
	if len(runes) > width-2 {
		runes = append(runes[:width-3], '…')
	}
	return " " + string(runes) + strings.Repeat(" ", width-len(runes)-1)
}

func inlineIdleText(content string) string {
	return strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(content)
}
