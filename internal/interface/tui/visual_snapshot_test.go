package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestVisualRuntimeNoColorSnapshots(t *testing.T) {
	ctx := noColorRenderContext()
	ctx.Width = 48

	exec := newToolHistoryCell()
	exec.Apply(event.ToolCallStarted{CallID: "exec", ToolName: "execute_command", SideEffect: "write", Detail: "go test ./..."})
	exec.Apply(event.ToolCallCompleted{CallID: "exec", ToolName: "execute_command", Success: true, Duration: 1250 * time.Millisecond, Summary: "ok"})

	explore := newToolHistoryCell()
	explore.Apply(event.ToolCallStarted{CallID: "read", ToolName: "read_file", SideEffect: "read", ActionSummary: "Read docs/design.md"})
	explore.Apply(event.ToolCallCompleted{CallID: "read", ToolName: "read_file", Success: true})

	search := newToolHistoryCell()
	search.Apply(event.ToolCallStarted{CallID: "web", ToolName: "web_search", SideEffect: "network", ActionSummary: "Amadeus TUI"})
	search.Apply(event.ToolCallCompleted{CallID: "web", ToolName: "web_search", Success: true})

	for name, test := range map[string]struct{ got, want string }{
		"working":   {got: activityIndicator(ctx.Now, ctx.MotionStart, ctx.Motion, ctx.Palette) + " " + shimmerText("Working", ctx.Now, ctx.MotionStart, ctx.Motion, ctx.Palette) + " (1m 05s • esc to interrupt)", want: "• Working (1m 05s • esc to interrupt)"},
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
		Task: func(context.Context, TaskSubmission) error { return nil },
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
