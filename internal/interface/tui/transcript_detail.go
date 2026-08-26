package tui

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	xansi "github.com/charmbracelet/x/ansi"
)

const (
	defaultTranscriptDetailItemBytes = 32 * 1024
	defaultTranscriptDetailRunBytes  = 256 * 1024
)

var transcriptSensitiveValue = regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*bearer\s+\S+|\b(?:api[_-]?key|token|password)\s*[:=]\s*\S+|\bbearer\s+\S+`)

type transcriptDetail struct {
	ID        string
	Title     string
	Content   string
	LineCount int
	Truncated bool
	bytes     int
}

type transcriptDetailStore struct {
	items        []transcriptDetail
	itemBytes    int
	runBytes     int
	retainedByte int
}

func newTranscriptDetailStore(itemBytes, runBytes int) *transcriptDetailStore {
	if itemBytes <= 0 {
		itemBytes = defaultTranscriptDetailItemBytes
	}
	if runBytes <= 0 {
		runBytes = defaultTranscriptDetailRunBytes
	}
	if itemBytes > runBytes {
		itemBytes = runBytes
	}
	return &transcriptDetailStore{itemBytes: itemBytes, runBytes: runBytes}
}

func (store *transcriptDetailStore) Add(id, title, content string) (transcriptDetail, bool) {
	if store == nil {
		return transcriptDetail{}, false
	}
	id = strings.TrimSpace(id)
	content = sanitizeTranscriptDetail(content)
	if id == "" || strings.TrimSpace(content) == "" {
		return transcriptDetail{}, false
	}
	for index := range store.items {
		if store.items[index].ID == id {
			store.retainedByte -= store.items[index].bytes
			store.items = append(store.items[:index], store.items[index+1:]...)
			break
		}
	}
	projected, truncated := projectTranscriptDetail(content, store.itemBytes)
	item := transcriptDetail{
		ID: id, Title: strings.TrimSpace(title), Content: projected,
		LineCount: strings.Count(content, "\n") + 1, Truncated: truncated, bytes: len(projected),
	}
	store.items = append(store.items, item)
	store.retainedByte += item.bytes
	for store.retainedByte > store.runBytes && len(store.items) > 1 {
		store.retainedByte -= store.items[0].bytes
		store.items = store.items[1:]
	}
	return item, true
}

func (store *transcriptDetailStore) Empty() bool {
	return store == nil || len(store.items) == 0
}

func (store *transcriptDetailStore) Render() string {
	if store == nil || len(store.items) == 0 {
		return "No transcript details available."
	}
	parts := make([]string, 0, len(store.items))
	for _, item := range store.items {
		title := item.Title
		if title == "" {
			title = item.ID
		}
		header := "• " + title
		if item.Truncated {
			header += fmt.Sprintf(" · %d lines · bounded", item.LineCount)
		}
		parts = append(parts, header+"\n\n"+item.Content)
	}
	return strings.Join(parts, "\n\n────────────────────────────────────────\n\n")
}

func sanitizeTranscriptDetail(value string) string {
	value = transcriptSensitiveValue.ReplaceAllString(xansi.Strip(value), "[REDACTED]")
	var builder strings.Builder
	for _, character := range value {
		if character == '\n' || character == '\t' || !unicode.IsControl(character) {
			builder.WriteRune(character)
		}
	}
	return strings.TrimSpace(builder.String())
}

func projectTranscriptDetail(value string, maximumBytes int) (string, bool) {
	if maximumBytes <= 0 || len(value) <= maximumBytes {
		return value, false
	}
	const marker = "\n\n… [bounded transcript detail] …\n\n"
	available := maximumBytes - len(marker)
	if available <= 0 {
		return truncateUTF8Prefix(value, maximumBytes), true
	}
	headBytes := available / 2
	tailBytes := available - headBytes
	return truncateUTF8Prefix(value, headBytes) + marker + truncateUTF8Suffix(value, tailBytes), true
}

func truncateUTF8Prefix(value string, maximumBytes int) string {
	if maximumBytes <= 0 {
		return ""
	}
	if len(value) <= maximumBytes {
		return value
	}
	end := maximumBytes
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}

func truncateUTF8Suffix(value string, maximumBytes int) string {
	if maximumBytes <= 0 {
		return ""
	}
	if len(value) <= maximumBytes {
		return value
	}
	start := len(value) - maximumBytes
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:]
}
