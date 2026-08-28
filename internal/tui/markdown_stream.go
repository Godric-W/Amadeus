package tui

import "strings"

// MarkdownStreamCollector accumulates one retry attempt's raw source. It only
// exposes newline-terminated source for live rendering; finalize returns the
// unmodified source so the completed item can remain authoritative.
type MarkdownStreamCollector struct {
	source             strings.Builder
	committedSourceLen int
}

func (collector *MarkdownStreamCollector) Push(delta string) bool {
	if collector == nil || delta == "" {
		return false
	}
	sourceStart := collector.source.Len()
	collector.source.WriteString(delta)
	lastNewline := strings.LastIndexByte(delta, '\n')
	if lastNewline < 0 {
		return false
	}
	commitEnd := sourceStart + lastNewline + 1
	if commitEnd <= collector.committedSourceLen {
		return false
	}
	collector.committedSourceLen = commitEnd
	return true
}

func (collector *MarkdownStreamCollector) CommittedSource() string {
	if collector == nil {
		return ""
	}
	source := collector.source.String()
	return source[:collector.committedSourceLen]
}

func (collector *MarkdownStreamCollector) Source() string {
	if collector == nil {
		return ""
	}
	return collector.source.String()
}

func (collector *MarkdownStreamCollector) Reset() {
	if collector == nil {
		return
	}
	collector.source.Reset()
	collector.committedSourceLen = 0
}
