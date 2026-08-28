package tui

import "sync"

type MarkdownRenderKey struct {
	Width   int
	Mode    HistoryRenderMode
	Palette terminalPalette
}

type markdownRenderCache struct {
	mu     sync.Mutex
	key    MarkdownRenderKey
	lines  []MarkdownLine
	filled bool
}

func (cache *markdownRenderCache) Render(key MarkdownRenderKey, render func() []MarkdownLine) []MarkdownLine {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.filled && cache.key == key {
		return cloneMarkdownLines(cache.lines)
	}
	cache.key, cache.lines, cache.filled = key, render(), true
	return cloneMarkdownLines(cache.lines)
}
