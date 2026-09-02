package tui

import (
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// TableAlignment is the presentation alignment declared by a GFM delimiter
// row. Default is left alignment, matching the Markdown terminal convention.
type TableAlignment uint8

const (
	TableAlignmentDefault TableAlignment = iota
	TableAlignmentLeft
	TableAlignmentCenter
	TableAlignmentRight
)

type TableColumnKind uint8

const (
	TableColumnNarrative TableColumnKind = iota
	TableColumnTokenHeavy
	TableColumnCompact
)

// MarkdownTableCell retains the structured projection of one AST table cell.
// Lines preserve rich spans, hard breaks and hyperlink destinations until the
// layout pass chooses a width.
type MarkdownTableCell struct {
	Lines        []MarkdownLine
	PlainText    string
	HardBreaks   []int
	Hyperlinks   []HyperlinkRange
	DisplayWidth int
}

// MarkdownTable is the source-backed table model owned by MarkdownWriter.
// Layout and streaming code consume this model but never parse Markdown.
type MarkdownTable struct {
	Header      []MarkdownTableCell
	Rows        [][]MarkdownTableCell
	Spillover   []MarkdownTableCell
	Alignments  []TableAlignment
	Prefix      string
	SourceRange MarkdownSourceRange
}

type MarkdownSourceRange struct {
	Start int
	End   int
}

type TableColumnMetrics struct {
	Kind                TableColumnKind
	HeaderWidth         int
	MaxBodyWidth        int
	PreferredWidth      int
	MinimumWidth        int
	PreferredFloor      int
	HeaderTokenWidth    int
	BodyTokenWidth      int
	BodyTokenCount      int
	LongBodyTokenCount  int
	AverageWordsPerCell float64
	AverageCellWidth    float64
}

type TablePresentation uint8

const (
	TablePresentationGrid TablePresentation = iota
	TablePresentationAlignedRecords
	TablePresentationStackedRecords
	TablePresentationPipeFallback
)

// MarkdownTableLayout is a derived, width-specific presentation. It is kept
// separate from MarkdownTable so resize/replay can rebuild layout without
// mutating source-backed cells.
type MarkdownTableLayout struct {
	ColumnWidths []int
	HeaderRows   [][]MarkdownLine
	BodyRows     [][][]MarkdownLine
	Presentation TablePresentation
}

const (
	minMarkdownTableColumnWidth = 3
	markdownTableCellPadding    = 1
	markdownTableColumnGap      = 2
	maxMarkdownTableColumns     = 12
	maxMarkdownTableRows        = 200
	maxMarkdownTableCellBytes   = 16 * 1024
	markdownTableSoftFloor      = 16
)

type markdownTableNode = *extast.Table

func (writer *MarkdownWriter) tableLines(table markdownTableNode, prefix string) []MarkdownLine {
	if table == nil {
		return nil
	}
	model := &MarkdownTable{Prefix: prefix, Alignments: make([]TableAlignment, len(table.Alignments))}
	if table.Lines().Len() > 0 {
		model.SourceRange = MarkdownSourceRange{Start: table.Lines().At(0).Start, End: table.Lines().At(table.Lines().Len() - 1).Stop}
	}
	for index, alignment := range table.Alignments {
		model.Alignments[index] = tableAlignment(alignment)
	}
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		header := false
		if _, ok := row.(*extast.TableHeader); ok {
			header = true
		}
		cells := make([]MarkdownTableCell, 0)
		for node := row.FirstChild(); node != nil; node = node.NextSibling() {
			style := stylePlain
			if header {
				style = styleBold
			}
			cellLines := rebuildMarkdownHyperlinks(writer.inlineLines(node, style))
			cell := makeMarkdownTableCell(cellLines)
			cells = append(cells, cell)
		}
		if !header && len(cells) == 1 && !tableRowHasPipeSyntax(row, writer.source) {
			model.Spillover = append(model.Spillover, cells[0])
			continue
		}
		if header {
			model.Header = cells
		} else {
			model.Rows = append(model.Rows, cells)
		}
	}
	if len(model.Header) == 0 && len(model.Rows) == 0 {
		return nil
	}
	metrics := collectTableColumnMetrics(model, maxInt(len(model.Header), tableModelColumnCount(model)))
	widths := make([]int, len(metrics))
	for index, metric := range metrics {
		widths[index] = maxInt(metric.MinimumWidth, metric.PreferredWidth)
	}
	lines := renderTableGrid(model, normalizeTableRows(model), widths, 0)
	for index := range lines {
		lines[index].Table = model
	}
	return lines
}

func tableAlignment(value extast.Alignment) TableAlignment {
	switch value {
	case extast.AlignLeft:
		return TableAlignmentLeft
	case extast.AlignCenter:
		return TableAlignmentCenter
	case extast.AlignRight:
		return TableAlignmentRight
	default:
		return TableAlignmentDefault
	}
}

func makeMarkdownTableCell(lines []MarkdownLine) MarkdownTableCell {
	cell := MarkdownTableCell{Lines: cloneMarkdownLines(lines)}
	var plain strings.Builder
	for index, line := range lines {
		if index > 0 {
			plain.WriteByte(' ')
			cell.HardBreaks = append(cell.HardBreaks, plain.Len()-1)
		}
		text := markdownLineText(line)
		plain.WriteString(text)
		cell.DisplayWidth = maxInt(cell.DisplayWidth, uniseg.StringWidth(text))
		cell.Hyperlinks = append(cell.Hyperlinks, line.Hyperlinks...)
	}
	cell.PlainText = plain.String()
	return cell
}

func tablePreviewCell(cell MarkdownTableCell) MarkdownLine {
	if len(cell.Lines) == 0 {
		return MarkdownLine{}
	}
	line := cloneMarkdownLines(cell.Lines[:1])[0]
	for _, extra := range cell.Lines[1:] {
		line.Spans = append(line.Spans, MarkdownSpan{Text: " "})
		line.Spans = append(line.Spans, extra.Spans...)
	}
	return line
}

func tableModelColumnCount(table *MarkdownTable) int {
	columns := len(table.Header)
	for _, row := range table.Rows {
		columns = maxInt(columns, len(row))
	}
	return columns
}

func layoutMarkdownTables(lines []MarkdownLine, width int) []MarkdownLine {
	result := make([]MarkdownLine, 0, len(lines))
	for index := 0; index < len(lines); {
		table := lines[index].Table
		if table == nil {
			if !lines[index].TableRule {
				result = append(result, lines[index])
			}
			index++
			continue
		}
		for index < len(lines) && lines[index].Table == table {
			index++
		}
		result = append(result, layoutMarkdownTable(table, width)...)
	}
	return result
}

func layoutMarkdownTable(table *MarkdownTable, width int) []MarkdownLine {
	if table == nil {
		return nil
	}
	rows := normalizeTableRows(table)
	columns := len(table.Header)
	for _, row := range rows {
		columns = maxInt(columns, len(row))
	}
	if columns == 0 {
		return nil
	}
	if len(rows) > maxMarkdownTableRows || columns > maxMarkdownTableColumns || markdownTableCellsTooLargeTyped(table) {
		return renderTableRecords(table, width)
	}
	metrics := collectTableColumnMetrics(table, columns)
	available := maxInt(0, width-uniseg.StringWidth(table.Prefix))
	columnWidths, ok := computeTableColumnWidths(metrics, available)
	if !ok || tableShouldUseRecords(table, metrics, columnWidths) {
		return renderTableRecords(table, width)
	}
	return renderTableGrid(table, rows, columnWidths, width)
}

func normalizeTableRows(table *MarkdownTable) [][]MarkdownTableCell {
	columns := len(table.Header)
	for _, row := range table.Rows {
		columns = maxInt(columns, len(row))
	}
	rows := make([][]MarkdownTableCell, len(table.Rows))
	for index, row := range table.Rows {
		rows[index] = make([]MarkdownTableCell, columns)
		copy(rows[index], row)
	}
	return rows
}

func tableRowHasPipeSyntax(row ast.Node, source []byte) bool {
	if row == nil || row.Lines().Len() == 0 {
		return true
	}
	start := row.Lines().At(0).Start
	end := row.Lines().At(row.Lines().Len() - 1).Stop
	if start < 0 || end < start || end > len(source) {
		return true
	}
	return countUnescapedPipes(string(source[start:end])) >= 2
}

func collectTableColumnMetrics(table *MarkdownTable, columns int) []TableColumnMetrics {
	metrics := make([]TableColumnMetrics, columns)
	for column := range metrics {
		metrics[column].MinimumWidth = minMarkdownTableColumnWidth
		metrics[column].HeaderWidth = tableCellWidthAt(table.Header, column)
		headerText := strings.TrimSpace(tableCellTextAt(table.Header, column))
		metrics[column].HeaderTokenWidth = longestTableTokenWidth(headerText)
		var totalWords, totalCells, totalCellWidth int
		for _, row := range table.Rows {
			cellWidth := tableCellWidthAt(row, column)
			metrics[column].MaxBodyWidth = maxInt(metrics[column].MaxBodyWidth, cellWidth)
			if column >= len(row) {
				continue
			}
			text := strings.TrimSpace(row[column].PlainText)
			wordCount := 0
			for _, token := range strings.Fields(text) {
				tokenWidth := uniseg.StringWidth(token)
				metrics[column].BodyTokenWidth = maxInt(metrics[column].BodyTokenWidth, tokenWidth)
				metrics[column].BodyTokenCount++
				metrics[column].LongBodyTokenCount += boolInt(tokenWidth >= 20)
				wordCount++
			}
			if wordCount > 0 {
				totalWords += wordCount
				totalCells++
				totalCellWidth += cellWidth
			}
		}
		if totalCells == 0 {
			metrics[column].AverageWordsPerCell = float64(len(strings.Fields(headerText)))
			metrics[column].AverageCellWidth = float64(metrics[column].HeaderWidth)
		} else {
			metrics[column].AverageWordsPerCell = float64(totalWords) / float64(totalCells)
			metrics[column].AverageCellWidth = float64(totalCellWidth) / float64(totalCells)
		}
		metrics[column].PreferredWidth = maxInt(metrics[column].HeaderWidth, metrics[column].MaxBodyWidth)
		metrics[column].Kind = classifyTableColumn(metrics[column])
		if metrics[column].PreferredWidth < minMarkdownTableColumnWidth {
			metrics[column].PreferredWidth = minMarkdownTableColumnWidth
		}
		floor := markdownTableSoftFloor
		if metrics[column].Kind == TableColumnCompact {
			floor = maxInt(metrics[column].HeaderTokenWidth, minInt(metrics[column].BodyTokenWidth, markdownTableSoftFloor))
		}
		metrics[column].PreferredFloor = maxInt(metrics[column].MinimumWidth, minInt(metrics[column].PreferredWidth, floor))
	}
	return metrics
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func tableCellWidthAt(row []MarkdownTableCell, column int) int {
	if column < 0 || column >= len(row) {
		return 0
	}
	return row[column].DisplayWidth
}

func classifyTableColumn(metrics TableColumnMetrics) TableColumnKind {
	if metrics.LongBodyTokenCount > 0 && metrics.LongBodyTokenCount >= metrics.BodyTokenCount-metrics.LongBodyTokenCount {
		return TableColumnTokenHeavy
	}
	if metrics.AverageWordsPerCell >= 4 || metrics.AverageCellWidth >= 28 {
		return TableColumnNarrative
	}
	return TableColumnCompact
}

func tableCellTextAt(row []MarkdownTableCell, column int) string {
	if column < 0 || column >= len(row) {
		return ""
	}
	return row[column].PlainText
}

func markdownTableTokenHeavy(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || strings.IndexFunc(text, unicode.IsSpace) >= 0 {
		return false
	}
	return strings.Contains(text, "://") || strings.ContainsAny(text, `/\\?#=&%_`) || strings.Count(text, "-") > 1 || len(text) >= 16
}

func longestTableTokenWidth(text string) int {
	longest := 0
	for _, token := range strings.Fields(text) {
		longest = maxInt(longest, uniseg.StringWidth(token))
	}
	return longest
}

func computeTableColumnWidths(metrics []TableColumnMetrics, available int) ([]int, bool) {
	if len(metrics) == 0 {
		return nil, false
	}
	overhead := len(metrics)*2*markdownTableCellPadding + (len(metrics)-1)*markdownTableColumnGap
	contentBudget := available - overhead
	minimum := 0
	for _, metric := range metrics {
		minimum += metric.MinimumWidth
	}
	if contentBudget < minimum {
		return nil, false
	}
	widths := make([]int, len(metrics))
	for index, metric := range metrics {
		widths[index] = maxInt(metric.MinimumWidth, metric.PreferredWidth)
	}
	if sumTableWidths(widths) <= contentBudget {
		return widths, true
	}
	for _, kind := range []TableColumnKind{TableColumnTokenHeavy, TableColumnNarrative, TableColumnCompact} {
		for sumTableWidths(widths) > contentBudget {
			candidate := -1
			for index, metric := range metrics {
				if metric.Kind != kind {
					continue
				}
				floor := metric.PreferredFloor
				if widths[index] > floor && (candidate < 0 || widths[index] > widths[candidate]) {
					candidate = index
				}
			}
			if candidate < 0 {
				break
			}
			widths[candidate]--
		}
	}
	// Soft floors are preferred readability targets, not a reason to abandon a
	// grid when the viewport can still fit the hard minimum. A second pass may
	// compress every class down to the minimum before records fallback.
	for sumTableWidths(widths) > contentBudget {
		candidate := -1
		for index, metric := range metrics {
			if widths[index] > metric.MinimumWidth && (candidate < 0 || widths[index] > widths[candidate]) {
				candidate = index
			}
		}
		if candidate < 0 {
			break
		}
		widths[candidate]--
	}
	if sumTableWidths(widths) > contentBudget {
		return nil, false
	}
	return widths, true
}

func tableShouldUseRecords(table *MarkdownTable, metrics []TableColumnMetrics, widths []int) bool {
	if len(table.Rows) == 0 {
		return false
	}
	affectedRows := 0
	for _, row := range table.Rows {
		affected := false
		tallExpansive := 0
		for column, cell := range row {
			if column >= len(widths) {
				continue
			}
			fragmented := longestTableTokenWidth(cell.PlainText) > widths[column]
			switch metrics[column].Kind {
			case TableColumnCompact:
				affected = affected || fragmented
			case TableColumnTokenHeavy:
				affected = affected || (widths[column] < 12 && fragmented)
			}
			if metrics[column].Kind != TableColumnCompact {
				height := len(wrapTableCell(cell, widths[column]))
				if height >= 4 {
					tallExpansive++
				}
				if metrics[column].Kind == TableColumnNarrative && widths[column] < 12 && height >= 7 {
					affected = true
				}
				if metrics[column].Kind == TableColumnNarrative && widths[column] < 6 && height >= 4 {
					affected = true
				}
			}
		}
		if tallExpansive >= 2 {
			affected = true
		}
		if affected {
			affectedRows++
		}
	}
	return affectedRows > 0 && affectedRows*2 >= len(table.Rows)
}

func renderTableGrid(table *MarkdownTable, rows [][]MarkdownTableCell, widths []int, width int) []MarkdownLine {
	result := make([]MarkdownLine, 0)
	if len(table.Header) > 0 {
		result = append(result, renderTablePhysicalRow(table.Header, widths, table.Alignments, table.Prefix, true)...)
		result = append(result, tableSeparatorLine(table.Prefix, widths, '━', width))
	}
	for rowIndex, row := range rows {
		result = append(result, renderTablePhysicalRow(row, widths, table.Alignments, table.Prefix, false)...)
		if rowIndex+1 < len(rows) {
			result = append(result, tableSeparatorLine(table.Prefix, widths, '─', width))
		}
	}
	result = append(result, renderTableSpillover(table)...)
	return result
}

func renderTableSpillover(table *MarkdownTable) []MarkdownLine {
	result := make([]MarkdownLine, 0, len(table.Spillover))
	for _, cell := range table.Spillover {
		for _, line := range cell.Lines {
			line.InitialIndent = append(markdownIndentSpans(table.Prefix), line.InitialIndent...)
			line.SubsequentIndent = append(markdownIndentSpans(table.Prefix), line.SubsequentIndent...)
			line.BlockKind = markdownBlockProse
			result = append(result, line)
		}
	}
	return result
}

func renderTablePhysicalRow(row []MarkdownTableCell, widths []int, alignments []TableAlignment, prefix string, header bool) []MarkdownLine {
	wrapped := make([][]MarkdownLine, len(widths))
	height := 1
	for column, contentWidth := range widths {
		if column < len(row) {
			wrapped[column] = wrapTableCell(row[column], contentWidth)
		}
		if len(wrapped[column]) == 0 {
			wrapped[column] = []MarkdownLine{{}}
		}
		height = maxInt(height, len(wrapped[column]))
	}
	lastColumn := -1
	for column, lines := range wrapped {
		for _, line := range lines {
			if uniseg.StringWidth(markdownLineText(line)) > 0 {
				lastColumn = column
				break
			}
		}
	}
	result := make([]MarkdownLine, 0, height)
	for rowLine := 0; rowLine < height; rowLine++ {
		if lastColumn < 0 {
			result = append(result, MarkdownLine{InitialIndent: markdownIndentSpans(prefix), SubsequentIndent: markdownIndentSpans(prefix), BlockKind: markdownBlockTable})
			continue
		}
		line := MarkdownLine{InitialIndent: markdownIndentSpans(prefix), SubsequentIndent: markdownIndentSpans(prefix), BlockKind: markdownBlockTable}
		for column, contentWidth := range widths[:lastColumn+1] {
			if column > 0 {
				line.Spans = append(line.Spans, MarkdownSpan{Text: strings.Repeat(" ", markdownTableColumnGap)})
			}
			line.Spans = append(line.Spans, MarkdownSpan{Text: " "})
			cellLine := MarkdownLine{}
			if rowLine < len(wrapped[column]) {
				cellLine = wrapped[column][rowLine]
			}
			cellWidth := uniseg.StringWidth(markdownLineText(cellLine))
			left, right := tableAlignmentPadding(alignmentAt(alignments, column), contentWidth-cellWidth)
			if left > 0 {
				line.Spans = append(line.Spans, MarkdownSpan{Text: strings.Repeat(" ", left)})
			}
			line.Spans = append(line.Spans, cellLine.Spans...)
			if column < lastColumn && right > 0 {
				line.Spans = append(line.Spans, MarkdownSpan{Text: strings.Repeat(" ", right)})
			}
			if column < lastColumn {
				line.Spans = append(line.Spans, MarkdownSpan{Text: " "})
			}
		}
		if header {
			for index := range line.Spans {
				line.Spans[index].Markdown.Bold = true
				if strings.TrimSpace(line.Spans[index].Text) != "" {
					line.Spans[index].Style = styleAccent
				}
			}
		}
		result = append(result, line)
	}
	return result
}

func wrapTableCell(cell MarkdownTableCell, width int) []MarkdownLine {
	result := make([]MarkdownLine, 0)
	for _, line := range cell.Lines {
		result = append(result, wrapMarkdownLine(line, maxInt(1, width))...)
	}
	if len(result) == 0 {
		result = append(result, MarkdownLine{})
	}
	return result
}

func alignmentAt(alignments []TableAlignment, column int) TableAlignment {
	if column < 0 || column >= len(alignments) {
		return TableAlignmentDefault
	}
	return alignments[column]
}

func tableAlignmentPadding(alignment TableAlignment, extra int) (int, int) {
	if extra <= 0 {
		return 0, 0
	}
	switch alignment {
	case TableAlignmentRight:
		return extra, 0
	case TableAlignmentCenter:
		left := extra / 2
		return left, extra - left
	default:
		return 0, extra
	}
}

func tableSeparatorLine(prefix string, widths []int, glyph rune, _ int) MarkdownLine {
	line := MarkdownLine{InitialIndent: markdownIndentSpans(prefix), SubsequentIndent: markdownIndentSpans(prefix), TableRule: true, BlockKind: markdownBlockTable}
	for column, columnWidth := range widths {
		if column > 0 {
			line.Spans = append(line.Spans, MarkdownSpan{Text: strings.Repeat(" ", markdownTableColumnGap)})
		}
		line.Spans = append(line.Spans, MarkdownSpan{Text: strings.Repeat(" ", markdownTableCellPadding)})
		line.Spans = append(line.Spans, MarkdownSpan{Text: strings.Repeat(string(glyph), columnWidth)})
		line.Spans = append(line.Spans, MarkdownSpan{Text: strings.Repeat(" ", markdownTableCellPadding)})
	}
	return line
}

func renderTableRecords(table *MarkdownTable, width int) []MarkdownLine {
	if len(table.Rows) == 0 {
		if len(table.Header) == 0 {
			return nil
		}
		return []MarkdownLine{renderPipeHeader(table, width)}
	}
	prefixWidth := uniseg.StringWidth(table.Prefix)
	available := maxInt(1, width-prefixWidth)
	labels := make([]string, len(table.Header))
	labelWidth := 0
	for column := range labels {
		labels[column] = strings.TrimSpace(table.Header[column].PlainText)
		if labels[column] == "" {
			labels[column] = "Column " + itoa(column+1)
		}
		labelWidth = maxInt(labelWidth, uniseg.StringWidth(labels[column]))
	}
	if labelWidth+3 < available {
		return append(renderAlignedRecords(table, labels, labelWidth, available), renderTableSpillover(table)...)
	}
	return append(renderStackedRecords(table, labels, available), renderTableSpillover(table)...)
}

func renderAlignedRecords(table *MarkdownTable, labels []string, labelWidth, available int) []MarkdownLine {
	result := make([]MarkdownLine, 0)
	valueWidth := maxInt(1, available-labelWidth-2)
	for rowIndex, row := range table.Rows {
		for column, cell := range row {
			label := "Column " + itoa(column+1)
			if column < len(labels) {
				label = labels[column]
			}
			labelPadding := labelWidth - uniseg.StringWidth(label)
			valueLines := wrapTableCell(cell, valueWidth)
			for lineIndex, value := range valueLines {
				line := MarkdownLine{InitialIndent: markdownIndentSpans(table.Prefix), SubsequentIndent: markdownIndentSpans(table.Prefix), BlockKind: markdownBlockTable}
				if lineIndex == 0 {
					line.Spans = append(line.Spans, MarkdownSpan{Text: label + ":", Style: styleBold, Markdown: MarkdownStyle{Bold: true}})
					line.Spans = append(line.Spans, MarkdownSpan{Text: strings.Repeat(" ", labelPadding+2)})
				} else {
					line.Spans = append(line.Spans, MarkdownSpan{Text: strings.Repeat(" ", labelWidth+2)})
				}
				line.Spans = append(line.Spans, value.Spans...)
				result = append(result, line)
			}
		}
		if rowIndex < len(table.Rows)-1 {
			result = append(result, tableSeparatorLine(table.Prefix, []int{maxInt(1, available-2*markdownTableCellPadding)}, '─', available))
		}
	}
	return result
}

func renderStackedRecords(table *MarkdownTable, labels []string, available int) []MarkdownLine {
	result := make([]MarkdownLine, 0)
	for rowIndex, row := range table.Rows {
		for column, cell := range row {
			label := "Column " + itoa(column+1)
			if column < len(labels) {
				label = labels[column]
			}
			result = append(result, MarkdownLine{InitialIndent: markdownIndentSpans(table.Prefix), SubsequentIndent: markdownIndentSpans(table.Prefix), Spans: []MarkdownSpan{{Text: label + ":", Style: styleBold, Markdown: MarkdownStyle{Bold: true}}}, BlockKind: markdownBlockTable})
			for _, value := range wrapTableCell(cell, maxInt(1, available-2)) {
				result = append(result, MarkdownLine{InitialIndent: markdownIndentSpans(table.Prefix + "  "), SubsequentIndent: markdownIndentSpans(table.Prefix + "  "), Spans: value.Spans, BlockKind: markdownBlockTable})
			}
		}
		if rowIndex < len(table.Rows)-1 {
			result = append(result, tableSeparatorLine(table.Prefix, []int{maxInt(1, available-2*markdownTableCellPadding)}, '─', available))
		}
	}
	return result
}

func renderPipeHeader(table *MarkdownTable, width int) MarkdownLine {
	line := MarkdownLine{InitialIndent: markdownIndentSpans(table.Prefix), SubsequentIndent: markdownIndentSpans(table.Prefix), BlockKind: markdownBlockTable}
	line.Spans = append(line.Spans, MarkdownSpan{Text: "| ", Style: styleDim})
	for column, cell := range table.Header {
		if column > 0 {
			line.Spans = append(line.Spans, MarkdownSpan{Text: " | ", Style: styleDim})
		}
		line.Spans = append(line.Spans, tablePreviewCell(cell).Spans...)
	}
	line.Spans = append(line.Spans, MarkdownSpan{Text: " |", Style: styleDim})
	if uniseg.StringWidth(markdownLineText(line)) > width {
		line = MarkdownLine{InitialIndent: markdownIndentSpans(table.Prefix), Spans: []MarkdownSpan{{Text: "| table |", Style: styleDim}}, BlockKind: markdownBlockTable}
	}
	return line
}

func markdownTableCellsTooLargeTyped(table *MarkdownTable) bool {
	for _, cell := range table.Header {
		if len(cell.PlainText) > maxMarkdownTableCellBytes {
			return true
		}
	}
	for _, row := range table.Rows {
		for _, cell := range row {
			if len(cell.PlainText) > maxMarkdownTableCellBytes {
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

// The fence helpers are parser-input utilities. They are intentionally
// conservative and never mutate MarkdownSource.
func tableHoldbackStart(source string) int {
	if start, ok := parserTableHoldbackStart(source); ok {
		return start
	}
	lineStart := 0
	pendingHeader := -1
	previousHeader := -1
	inFence := false
	for _, line := range strings.SplitAfter(source, "\n") {
		trimmed := tableLineContent(line)
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

func tableLineContent(line string) string {
	trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
	for strings.HasPrefix(trimmed, ">") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
	}
	return trimmed
}

// parserTableHoldbackStart uses the same Goldmark parser as the final writer.
// A table remains mutable while it is the final top-level block; once another
// block follows, its layout can enter the stable streaming queue.
func parserTableHoldbackStart(source string) (int, bool) {
	parseSource := markdownParseSource(source)
	if parseSource == "" {
		return 0, false
	}
	// Unwrapping a fenced Markdown table changes byte offsets. Keep the whole
	// source mutable until completion rather than applying an invalid offset.
	if unwrapMarkdownTableFences(parseSource) != parseSource {
		return 0, true
	}
	document := newMarkdownRenderer().parser.Parse(text.NewReader([]byte(parseSource)), parser.WithContext(parser.NewContext()))
	last := document.LastChild()
	if last == nil {
		return 0, false
	}
	start := -1
	_ = ast.Walk(last, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if _, ok := node.(*extast.Table); !ok {
			return ast.WalkContinue, nil
		}
		table := node.(*extast.Table)
		if table.Lines().Len() > 0 {
			start = markdownSourceLineStart([]byte(parseSource), table.Lines().At(0).Start)
		}
		return ast.WalkContinue, nil
	})
	return start, start >= 0
}

func isMarkdownTableHeader(line string) bool {
	return countUnescapedPipes(line) >= 2 && !isMarkdownTableDelimiter(line)
}

func countUnescapedPipes(line string) int {
	count := 0
	escaped := false
	for _, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '|' {
			count++
		}
	}
	return count
}

func isMarkdownTableDelimiter(line string) bool {
	line = strings.Trim(line, " |")
	if line == "" {
		return false
	}
	for _, field := range splitUnescapedPipes(line) {
		field = strings.TrimSpace(field)
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

func splitUnescapedPipes(line string) []string {
	fields := make([]string, 0, 4)
	start := 0
	escaped := false
	for index, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '|' {
			fields = append(fields, line[start:index])
			start = index + 1
		}
	}
	fields = append(fields, line[start:])
	return fields
}

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
		if isMarkdownTableHeader(tableLineContent(lines[index])) && isMarkdownTableDelimiter(tableLineContent(lines[index+1])) {
			return true
		}
	}
	return false
}
