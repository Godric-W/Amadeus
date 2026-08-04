package tui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTranscriptDetailStoreBoundsItemsAndRun(t *testing.T) {
	store := newTranscriptDetailStore(80, 140)
	first, ok := store.Add("one", "Read one", strings.Repeat("你好 api_key=value ", 20))
	if !ok || !first.Truncated || len(first.Content) > 80 || !utf8.ValidString(first.Content) {
		t.Fatalf("first detail = %#v", first)
	}
	if strings.Contains(first.Content, "api_key=value") {
		t.Fatalf("detail leaked sensitive value: %q", first.Content)
	}
	store.Add("two", "Read two", strings.Repeat("second line\n", 20))
	store.Add("three", "Read three", strings.Repeat("third line\n", 20))
	if store.retainedByte > 140 || len(store.items) >= 3 {
		t.Fatalf("run budget not enforced: bytes=%d items=%d", store.retainedByte, len(store.items))
	}
}

func TestTranscriptDetailStoreReplacesDuplicateCall(t *testing.T) {
	store := newTranscriptDetailStore(1024, 4096)
	store.Add("call", "first", "old")
	store.Add("call", "second", "new")
	if len(store.items) != 1 || !strings.Contains(store.Render(), "second") || strings.Contains(store.Render(), "old") {
		t.Fatalf("duplicate replacement failed: %#v", store.items)
	}
}
