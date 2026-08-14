package builtin

import (
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestBoundRenderedItemsReportsByteAndTokenLimits(t *testing.T) {
	items := []string{"aaaa", "bbbb", "cccc"}
	render := func(values []string) string { return strings.Join(values, "\n") }

	bounded, text, truncated, reason := boundRenderedItems(items, render, 9, 100)
	if len(bounded) != 2 || text != "aaaa\nbbbb" || !truncated || reason != tool.TruncationByteLimit {
		t.Fatalf("unexpected byte bound: items=%#v text=%q truncated=%v reason=%q", bounded, text, truncated, reason)
	}

	bounded, text, truncated, reason = boundRenderedItems(items, render, 100, 2)
	if len(bounded) != 1 || text != "aaaa" || !truncated || reason != tool.TruncationTokenLimit {
		t.Fatalf("unexpected token bound: items=%#v text=%q truncated=%v reason=%q", bounded, text, truncated, reason)
	}
}
