package tui

type ContextCompactedCell struct{}

func NewContextCompactedCell() HistoryCell { return ContextCompactedCell{} }

func (ContextCompactedCell) DisplayLines(HistoryRenderContext) []styledLine {
	return []styledLine{{{Text: "• Context compacted", Style: styleDim}}}
}

func (ContextCompactedCell) RawLines() []string         { return []string{"Context compacted"} }
func (ContextCompactedCell) IsStreamContinuation() bool { return false }
