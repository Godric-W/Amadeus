package tui

import (
	"strings"

	"github.com/rivo/uniseg"
	extast "github.com/yuin/goldmark/extension/ast"
)

// markdownTableNode keeps table handling isolated from generic block walking.
// The first AB version uses a bounded row projection; richer column layout is
// intentionally a later presentation feature.
type markdownTableNode = *extast.Table

const (
	minMarkdownTableColumnWidth = 6
	maxMarkdownTableColumns     = 12
	maxMarkdownTableRows        = 200
	maxMarkdownTableCellBytes   = 16 * 1024
)

func (writer *MarkdownWriter) tableLines(table markdownTableNode, prefix string) []MarkdownLine {
	var rows [][]MarkdownLine
	headerRows := 0
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []MarkdownLine
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			style := stylePlain
			if _, header := row.(*extast.TableHeader); header {
				style = styleBold
				headerRows = len(rows) + 1
			}
			cells = append(cells, writer.inlineLine(cell, style))
		}
		rows = append(rows, cells)
	}
	widths := make([]int, 0)
	for _, row := range rows {
		for column, cell := range row {
			for len(widths) <= column {
				widths = append(widths, 0)
			}
			widths[column] = maxInt(widths[column], uniseg.StringWidth(markdownLineText(cell)))
		}
	}
	var headers []string
	if headerRows > 0 {
		for _, cell := range rows[headerRows-1] {
			headers = append(headers, markdownLineText(cell))
		}
	}
	var lines []MarkdownLine
	for rowIndex, row := range rows {
		line := prependMarkdownLine(prefix, renderTableRow(row, widths))
		line.TableCells = cloneMarkdownLines(row)
		line.TableHeader = append([]string(nil), headers...)
		line.TablePrefix = prefix
		lines = append(lines, line)
		if rowIndex+1 == headerRows {
			lines = append(lines, MarkdownLine{Spans: []MarkdownSpan{{Text: prefix + tableRule(widths), Style: styleDim}}, TableRule: true})
		}
	}
	return lines
}

func renderTableRow(cells []MarkdownLine, widths []int) MarkdownLine {
	line := MarkdownLine{}
	for index, cell := range cells {
		if index > 0 {
			line.Spans = append(line.Spans, MarkdownSpan{Text: " │ ", Style: styleDim})
		}
		line.Spans = append(line.Spans, cell.Spans...)
		padding := widths[index] - uniseg.StringWidth(markdownLineText(cell))
		if padding > 0 {
			line.Spans = append(line.Spans, MarkdownSpan{Text: strings.Repeat(" ", padding), Style: stylePlain})
		}
	}
	return line
}

func tableRule(widths []int) string {
	parts := make([]string, len(widths))
	for index, width := range widths {
		parts[index] = strings.Repeat("─", maxInt(3, width))
	}
	return strings.Join(parts, "─┼─")
}

func layoutMarkdownTables(lines []MarkdownLine, width int) []MarkdownLine {
	result := make([]MarkdownLine, 0, len(lines))
	for index := 0; index < len(lines); {
		if len(lines[index].TableCells) == 0 {
			if !lines[index].TableRule {
				result = append(result, lines[index])
			}
			index++
			continue
		}
		prefix := lines[index].TablePrefix
		headers := append([]string(nil), lines[index].TableHeader...)
		var rows [][]MarkdownLine
		for index < len(lines) && (len(lines[index].TableCells) > 0 || lines[index].TableRule) {
			if len(lines[index].TableCells) > 0 {
				rows = append(rows, lines[index].TableCells)
			}
			index++
		}
		result = append(result, layoutTableRows(rows, headers, prefix, width)...)
	}
	return result
}

func layoutTableRows(rows [][]MarkdownLine, headers []string, prefix string, width int) []MarkdownLine {
	if len(rows) == 0 {
		return nil
	}
	columns := 0
	for _, row := range rows {
		columns = maxInt(columns, len(row))
	}
	if columns == 0 {
		return nil
	}
	if len(rows) > maxMarkdownTableRows || columns > maxMarkdownTableColumns || markdownTableCellsTooLarge(rows) {
		return tableKeyValueRows(rows, headers, prefix, width)
	}
	widths := make([]int, columns)
	for _, row := range rows {
		for column, cell := range row {
			widths[column] = maxInt(widths[column], uniseg.StringWidth(markdownLineText(cell)))
		}
	}
	gaps := maxInt(0, columns-1) * 3
	available := maxInt(1, width-uniseg.StringWidth(prefix)-gaps)
	if available < columns*minMarkdownTableColumnWidth {
		return tableKeyValueRows(rows, headers, prefix, width)
	}
	for sumTableWidths(widths) > available {
		largest := -1
		for column, columnWidth := range widths {
			if columnWidth > minMarkdownTableColumnWidth && (largest < 0 || columnWidth > widths[largest]) {
				largest = column
			}
		}
		if largest < 0 {
			return tableKeyValueRows(rows, headers, prefix, width)
		}
		widths[largest]--
	}
	result := make([]MarkdownLine, 0, len(rows)+1)
	for rowIndex, row := range rows {
		physical, fits := renderPhysicalTableRow(row, widths)
		if !fits {
			return tableKeyValueRows(rows, headers, prefix, width)
		}
		for _, line := range physical {
			result = append(result, prependMarkdownLine(prefix, line))
		}
		if rowIndex == 0 && len(headers) > 0 {
			result = append(result, prependMarkdownLine(prefix, MarkdownLine{Spans: []MarkdownSpan{{Text: tableRule(widths), Style: styleDim}}, TableRule: true, BlockKind: markdownBlockTable}))
		}
	}
	return result
}

func renderPhysicalTableRow(row []MarkdownLine, widths []int) ([]MarkdownLine, bool) {
	wrappedCells := make([][]MarkdownLine, len(widths))
	height := 1
	for column, columnWidth := range widths {
		cell := MarkdownLine{}
		if column < len(row) {
			cell = row[column]
		}
		wrappedCells[column] = wrapMarkdownLine(cell, columnWidth)
		if len(wrappedCells[column]) == 0 {
			wrappedCells[column] = []MarkdownLine{{}}
		}
		for _, line := range wrappedCells[column] {
			if uniseg.StringWidth(markdownLineText(line)) > columnWidth {
				return nil, false
			}
		}
		height = maxInt(height, len(wrappedCells[column]))
	}

	physical := make([]MarkdownLine, 0, height)
	for rowLine := 0; rowLine < height; rowLine++ {
		line := MarkdownLine{BlockKind: markdownBlockTable}
		for column, columnWidth := range widths {
			if column > 0 {
				line.Spans = append(line.Spans, MarkdownSpan{Text: " │ ", Style: styleDim})
			}
			cellLine := MarkdownLine{}
			if rowLine < len(wrappedCells[column]) {
				cellLine = wrappedCells[column][rowLine]
			}
			line.Spans = append(line.Spans, cellLine.Spans...)
			padding := columnWidth - uniseg.StringWidth(markdownLineText(cellLine))
			if padding > 0 {
				line.Spans = append(line.Spans, MarkdownSpan{Text: strings.Repeat(" ", padding), Style: stylePlain})
			}
		}
		physical = append(physical, line)
	}
	return physical, true
}

func markdownTableCellsTooLarge(rows [][]MarkdownLine) bool {
	for _, row := range rows {
		for _, cell := range row {
			if len(markdownSpansText(cell.Spans)) > maxMarkdownTableCellBytes {
				return true
			}
		}
	}
	return false
}

func sumTableWidths(widths []int) int {
	total := 0
	for _, width := range widths {
		total += width
	}
	return total
}

func tableKeyValueRows(rows [][]MarkdownLine, headers []string, prefix string, width int) []MarkdownLine {
	if len(rows) <= 1 {
		if len(rows) == 0 {
			return nil
		}
		return []MarkdownLine{prependMarkdownLine(prefix, renderTableRow(rows[0], makeTableIntrinsicWidths(rows)))}
	}
	result := make([]MarkdownLine, 0)
	for _, row := range rows[1:] {
		for column, cell := range row {
			label := "Column " + itoa(column+1)
			if column < len(headers) && headers[column] != "" {
				label = headers[column]
			}
			line := prependMarkdownLine(prefix, MarkdownLine{Spans: append([]MarkdownSpan{{Text: label + ": ", Style: styleBold, Markdown: MarkdownStyle{Bold: true}}}, cell.Spans...)})
			result = append(result, wrapMarkdownLines([]MarkdownLine{line}, width)...)
		}
		result = append(result, MarkdownLine{})
	}
	if len(result) > 0 {
		return result[:len(result)-1]
	}
	return result
}

func makeTableIntrinsicWidths(rows [][]MarkdownLine) []int {
	columns := 0
	for _, row := range rows {
		columns = maxInt(columns, len(row))
	}
	widths := make([]int, columns)
	for _, row := range rows {
		for column, cell := range row {
			widths[column] = maxInt(widths[column], uniseg.StringWidth(markdownLineText(cell)))
		}
	}
	return widths
}

// tableHoldbackStart identifies the portion of an append-only stream that may
// still reflow as a GFM pipe table. It is deliberately conservative: a final
// header-looking row remains mutable until the next non-table line or stream
// completion, matching Codex's PendingHeader behavior.
func tableHoldbackStart(source string) int {
	lineStart := 0
	pendingHeader := -1
	previousHeader := -1
	inFence := false
	for _, line := range strings.SplitAfter(source, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			lineStart += len(line)
			continue
		}
		if !inFence && isMarkdownTableHeader(trimmed) {
			pendingHeader = lineStart
			previousHeader = lineStart
		} else if !inFence && previousHeader >= 0 && isMarkdownTableDelimiter(trimmed) {
			return previousHeader
		} else if trimmed != "" {
			pendingHeader = -1
			previousHeader = -1
		}
		lineStart += len(line)
	}
	return pendingHeader
}

func isMarkdownTableHeader(line string) bool {
	return strings.Count(line, "|") >= 2 && !isMarkdownTableDelimiter(line)
}

func isMarkdownTableDelimiter(line string) bool {
	line = strings.Trim(line, " |")
	if line == "" {
		return false
	}
	for _, field := range strings.Split(line, "|") {
		field = strings.Trim(field, " ")
		if len(field) < 3 {
			return false
		}
		for _, character := range field {
			if character != '-' && character != ':' {
				return false
			}
		}
	}
	return true
}

// unwrapMarkdownTableFences is deliberately conservative. It changes only the
// parser input, never MarkdownSource: copy/resume keep the exact model text.
func unwrapMarkdownTableFences(source string) string {
	lines := strings.SplitAfter(source, "\n")
	var output strings.Builder
	for index := 0; index < len(lines); {
		if fence, language, ok := parseMarkdownFenceOpening(lines[index]); ok && (language == "md" || language == "markdown") {
			end := index + 1
			for end < len(lines) && !isMarkdownFenceClosing(lines[end], fence) {
				end++
			}
			if end < len(lines) && fencedBodyHasTable(lines[index+1:end]) {
				for _, line := range lines[index+1 : end] {
					output.WriteString(line)
				}
				index = end + 1
				continue
			}
		}
		output.WriteString(lines[index])
		index++
	}
	return output.String()
}

type markdownFenceDelimiter struct {
	marker byte
	length int
}

func parseMarkdownFenceOpening(line string) (markdownFenceDelimiter, string, bool) {
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	indent := 0
	for indent < len(line) && indent < 4 && line[indent] == ' ' {
		indent++
	}
	if indent > 3 || indent >= len(line) || (line[indent] != '`' && line[indent] != '~') {
		return markdownFenceDelimiter{}, "", false
	}
	marker := line[indent]
	end := indent
	for end < len(line) && line[end] == marker {
		end++
	}
	if end-indent < 3 {
		return markdownFenceDelimiter{}, "", false
	}
	info := strings.TrimSpace(line[end:])
	if marker == '`' && strings.ContainsRune(info, '`') {
		return markdownFenceDelimiter{}, "", false
	}
	language := ""
	if fields := strings.Fields(info); len(fields) > 0 {
		language = strings.ToLower(fields[0])
	}
	return markdownFenceDelimiter{marker: marker, length: end - indent}, language, true
}

func isMarkdownFenceClosing(line string, fence markdownFenceDelimiter) bool {
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	indent := 0
	for indent < len(line) && indent < 4 && line[indent] == ' ' {
		indent++
	}
	if indent > 3 || indent >= len(line) || line[indent] != fence.marker {
		return false
	}
	end := indent
	for end < len(line) && line[end] == fence.marker {
		end++
	}
	return end-indent >= fence.length && strings.TrimSpace(line[end:]) == ""
}

func fencedBodyHasTable(lines []string) bool {
	for index := 0; index+1 < len(lines); index++ {
		if isMarkdownTableHeader(strings.TrimSpace(strings.TrimSuffix(lines[index], "\n"))) && isMarkdownTableDelimiter(strings.TrimSpace(strings.TrimSuffix(lines[index+1], "\n"))) {
			return true
		}
	}
	return false
}
