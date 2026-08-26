package tui

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/app"
)

func TestRunRejectsNonTerminalBeforeBootstrap(t *testing.T) {
	_, err := Run(context.Background(), RunOptions{
		Target: app.ThreadTarget{Kind: app.ThreadTargetNew},
		Input:  strings.NewReader(""), Output: &bytes.Buffer{}, ErrorOutput: &bytes.Buffer{},
		IsTerminal: func(io.Reader) bool { return false },
	})
	if err == nil || err.Error() != "interactive Amadeus requires a terminal" {
		t.Fatalf("unexpected non-terminal error: %v", err)
	}
}
