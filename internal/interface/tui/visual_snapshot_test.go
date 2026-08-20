package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	application "github.com/Godric-W/Amadeus/internal/app"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestVisualRuntimeNoColorSnapshots(t *testing.T) {
	ctx := noColorRenderContext()
	ctx.Width = 48

	exec := newToolHistoryCell()
	execStarted := toolStartedMessage("exec", "execute_command", "write", "", "go test ./...")
	exec.Apply(execStarted)
	exec.Apply(toolCompletedMessage(execStarted, protocol.ItemStatusCompleted, "ok", "1.25s", false))

	explore := newToolHistoryCell()
	exploreStarted := toolStartedMessage("read", "read", "read", "Read docs/design.md", "")
	explore.Apply(exploreStarted)
	explore.Apply(toolCompletedMessage(exploreStarted, protocol.ItemStatusCompleted, "", "0s", false))

	search := newToolHistoryCell()
	searchStarted := toolStartedMessage("web", "web_search", "network", "Amadeus TUI", "")
	search.Apply(searchStarted)
	search.Apply(toolCompletedMessage(searchStarted, protocol.ItemStatusCompleted, "", "0s", false))

	for name, test := range map[string]struct{ got, want string }{
		"working":   {got: spinnerGlyph(ctx.Now, ctx.MotionStart, ctx.Motion, ctx.Palette) + shimmerText("Working", ctx.Now, ctx.MotionStart, ctx.Motion, ctx.Palette) + " (1m 05s • esc to interrupt)", want: "✻ Working (1m 05s • esc to interrupt)"},
		"exec":      {got: xansi.Strip(renderHistoryCellForTest(exec, ctx)), want: "• Ran go test ./...\n  └ ok"},
		"explore":   {got: xansi.Strip(renderHistoryCellForTest(explore, ctx)), want: "• Explored\n  └ Read docs/design.md"},
		"web":       {got: xansi.Strip(renderHistoryCellForTest(search, ctx)), want: "• Searched the web\n  └ Amadeus TUI"},
		"separator": {got: xansi.Strip(renderHistoryCellForTest(FinalMessageSeparator{Elapsed: 65 * time.Second}, ctx)), want: "─ Worked for 1m 05s ────────────────────────────"},
	} {
		t.Run(name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("snapshot mismatch\n got: %q\nwant: %q", test.got, test.want)
			}
		})
	}
}

func TestFullscreenRunKeepsMainScreenAndNativeMouse(t *testing.T) {
	var output bytes.Buffer
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: bytes.NewBufferString("/exit\r"), Output: &output, Width: 80, NoColor: true, DisableAnimations: true,
		Application: newFakeFullscreenApplication(),
		Snapshot:    application.ThreadViewSnapshot{Generation: 1, ThreadID: "thread-1", Model: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	rendered := output.String()
	for _, forbidden := range []string{"?1049h", "?1049l", "?1000h", "?1002h", "?1003h"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("main-screen contract emitted %q: %q", forbidden, rendered)
		}
	}
}
