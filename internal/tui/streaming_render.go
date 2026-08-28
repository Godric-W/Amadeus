package tui

// StreamingRender caches the live render of newline-committed source. Its
// stable boundary feeds StreamState.CommitQueue; rows are committed to the
// replaceable TranscriptSurface, never native terminal scrollback.
type StreamingRender struct {
	Lines             []MarkdownLine
	StableSourceLen   int
	StableRenderedLen int
	HasReferences     bool
	SourceTransform   bool
}

func (render *StreamingRender) Reset() {
	if render == nil {
		return
	}
	*render = StreamingRender{}
}

func (render *StreamingRender) Recompute(renderer markdownRenderer, source MarkdownSource, mode HistoryRenderMode) {
	if render == nil {
		return
	}
	analysis := renderer.Analyze(source.Text)
	boundary := markdownStableSourceBoundary(analysis, source.Text, mode)
	render.Lines = renderer.Render(source, mode)
	render.StableSourceLen = boundary
	render.StableRenderedLen = len(renderer.Render(newMarkdownSource(source.Text[:boundary], source.CWD), mode))
	render.HasReferences = analysis.HasReferences
	render.SourceTransform = analysis.SourceTransform
}

// Append advances the stable prefix only at an empty-line block boundary. The
// final block remains mutable so a later setext heading, list continuation, or
// fence delimiter can alter its structure without rewriting prior blocks.
func (render *StreamingRender) Append(renderer markdownRenderer, source MarkdownSource, mode HistoryRenderMode) bool {
	if render == nil {
		return false
	}
	if mode == HistoryRenderRaw {
		render.Recompute(renderer, source, mode)
		return false
	}
	if render.HasReferences || render.SourceTransform {
		render.Recompute(renderer, source, mode)
		return false
	}
	if render.StableSourceLen > len(source.Text) || render.StableRenderedLen > len(render.Lines) {
		render.Recompute(renderer, source, mode)
		return true
	}

	pendingText := source.Text[render.StableSourceLen:]
	analysis := renderer.Analyze(pendingText)
	if analysis.HasReferences || analysis.SourceTransform {
		hadStablePrefix := render.StableSourceLen > 0 || render.StableRenderedLen > 0
		render.Recompute(renderer, source, mode)
		return hadStablePrefix
	}
	pendingSource := newMarkdownSource(pendingText, source.CWD)
	pendingLines := renderer.Render(pendingSource, mode)
	lines := cloneMarkdownLines(render.Lines[:render.StableRenderedLen])
	if len(lines) > 0 && len(pendingLines) > 0 {
		lines = append(lines, MarkdownLine{})
	}
	pendingRenderStart := len(lines)
	lines = append(lines, pendingLines...)

	boundary := analysis.LastTopLevelBlockStart
	if boundary > 0 {
		stableLines := renderer.Render(newMarkdownSource(pendingText[:boundary], source.CWD), mode)
		render.StableSourceLen += boundary
		render.StableRenderedLen = pendingRenderStart + len(stableLines)
	}
	render.Lines = lines
	return false
}

func markdownStableSourceBoundary(analysis markdownParseAnalysis, source string, mode HistoryRenderMode) int {
	if mode == HistoryRenderRaw {
		return len(source)
	}
	if analysis.HasReferences || analysis.SourceTransform {
		return 0
	}
	return analysis.LastTopLevelBlockStart
}
