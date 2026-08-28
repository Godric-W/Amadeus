package tui

import "testing"

func TestMarkdownRenderCacheHitsAndInvalidatesByRenderKey(t *testing.T) {
	cache := markdownRenderCache{}
	key := MarkdownRenderKey{Width: 80, Mode: HistoryRenderRich, Palette: terminalPalette{Level: colorLevelANSI16, Dark: true}}
	calls := 0
	render := func() []MarkdownLine {
		calls++
		return []MarkdownLine{{Spans: []MarkdownSpan{{Text: "cached"}}}}
	}
	cache.Render(key, render)
	cache.Render(key, render)
	if calls != 1 {
		t.Fatalf("same render key calls=%d", calls)
	}
	cache.Render(MarkdownRenderKey{Width: 40, Mode: HistoryRenderRich, Palette: key.Palette}, render)
	if calls != 2 {
		t.Fatalf("width change calls=%d", calls)
	}
	cache.Render(MarkdownRenderKey{Width: 40, Mode: HistoryRenderRaw, Palette: key.Palette}, render)
	if calls != 3 {
		t.Fatalf("mode change calls=%d", calls)
	}
}
