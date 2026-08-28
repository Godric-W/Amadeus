package tui

import (
	"strings"

	"github.com/rivo/uniseg"
)

type SessionHeaderCell struct {
	Version   string
	Model     string
	Directory string
}

func NewSessionHeaderCell(version, model, directory string) HistoryCell {
	return SessionHeaderCell{
		Version: strings.TrimSpace(version), Model: strings.TrimSpace(model), Directory: strings.TrimSpace(directory),
	}
}

func (cell SessionHeaderCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	width := maxInt(20, ctx.Width)
	logo := strings.Split(strings.Trim(terminalLogo(width), "\r\n"), "\n")
	lines := make([]styledLine, 0, len(logo)+8)
	for _, line := range logo {
		lines = append(lines, styledLine{{Text: line, Style: stylePlain}})
	}
	lines = append(lines, styledLine{})

	boxWidth := minInt(width, 60)
	innerWidth := maxInt(12, boxWidth-4)
	borderWidth := innerWidth + 2
	lines = append(lines, styledLine{{Text: "╭" + strings.Repeat("─", borderWidth) + "╮", Style: styleDim}})
	title := styledLine{{Text: "│ ", Style: styleDim}, {Text: ">_ ", Style: styleDim}, {Text: "Amadeus", Style: styleBold}}
	if cell.Version != "" {
		title = append(title, styledSpan{Text: " (" + cell.Version + ")", Style: styleDim})
	}
	lines = append(lines, padSessionHeaderLine(title, innerWidth))
	lines = append(lines, padSessionHeaderLine(styledLine{{Text: "│ ", Style: styleDim}}, innerWidth))
	if cell.Model != "" {
		lines = append(lines, sessionHeaderMetadataLine("model:", cell.Model, innerWidth))
	}
	if cell.Directory != "" {
		lines = append(lines, sessionHeaderMetadataLine("directory:", cell.Directory, innerWidth))
	}
	lines = append(lines, styledLine{{Text: "╰" + strings.Repeat("─", borderWidth) + "╯", Style: styleDim}})
	return lines
}

func sessionHeaderMetadataLine(label, value string, width int) styledLine {
	const labelWidth = 11
	available := maxInt(1, width-labelWidth)
	value = truncateLine(value, available)
	line := styledLine{
		{Text: "│ ", Style: styleDim},
		{Text: label + strings.Repeat(" ", maxInt(1, labelWidth-uniseg.StringWidth(label))), Style: styleDim},
		{Text: value, Style: stylePlain},
	}
	return padSessionHeaderLine(line, width)
}

func padSessionHeaderLine(line styledLine, innerWidth int) styledLine {
	contentWidth := 0
	for index, span := range line {
		if index == 0 && span.Text == "│ " {
			continue
		}
		contentWidth += uniseg.StringWidth(span.Text)
	}
	line = append(line,
		styledSpan{Text: strings.Repeat(" ", maxInt(0, innerWidth-contentWidth)), Style: stylePlain},
		styledSpan{Text: " │", Style: styleDim},
	)
	return line
}

func (cell SessionHeaderCell) RawLines() []string {
	lines := []string{"Amadeus"}
	if cell.Model != "" {
		lines = append(lines, "model: "+cell.Model)
	}
	if cell.Directory != "" {
		lines = append(lines, "directory: "+cell.Directory)
	}
	return lines
}

func (SessionHeaderCell) IsStreamContinuation() bool { return false }
