package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
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
	fetch := newToolHistoryCell()
	fetchStarted := toolStartedMessage("fetch", "web_fetch", "network", "example.com", "")
	fetch.Apply(fetchStarted)
	fetch.Apply(toolCompletedMessage(fetchStarted, protocol.ItemStatusCompleted, "", "0s", false))

	for name, test := range map[string]struct{ got, want string }{
		"working":   {got: spinnerGlyph(ctx.Now, ctx.MotionStart, ctx.Motion, ctx.Palette) + shimmerText("Working", ctx.Now, ctx.MotionStart, ctx.Motion, ctx.Palette) + " (1m 05s • esc to interrupt)", want: "✻ Working (1m 05s • esc to interrupt)"},
		"exec":      {got: xansi.Strip(renderHistoryCellForTest(exec, ctx)), want: "• Ran go test ./...\n  └ ok"},
		"explore":   {got: xansi.Strip(renderHistoryCellForTest(explore, ctx)), want: "• Explored\n  └ Read docs/design.md"},
		"web":       {got: xansi.Strip(renderHistoryCellForTest(search, ctx)), want: "• Searched the web\n  └ Amadeus TUI"},
		"web-fetch": {got: xansi.Strip(renderHistoryCellForTest(fetch, ctx)), want: "• Fetched web content\n  └ example.com"},
		"separator": {got: xansi.Strip(renderHistoryCellForTest(FinalMessageSeparator{Elapsed: 65 * time.Second}, ctx)), want: "─ Worked for 1m 05s ────────────────────────────"},
	} {
		t.Run(name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("snapshot mismatch\n got: %q\nwant: %q", test.got, test.want)
			}
		})
	}
}

func TestTUIRunKeepsMainScreenAndNativeMouse(t *testing.T) {
	var output bytes.Buffer
	app, err := NewApplication(ApplicationOptions{
		Input: bytes.NewBufferString("/exit\r"), Output: &output, Width: 80, NoColor: true, DisableAnimations: true,
		Application: newFakeApplicationPort(),
		Snapshot: application.ThreadViewSnapshot{
			Generation: 1, SessionID: testutil.SessionID(1), ThreadID: testThreadID(1),
			Configuration: protocol.SessionConfiguration{Model: "test", Mode: protocol.ModeKindDefault},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	exitInfo, err := app.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if exitInfo.ExitReason != ExitReasonUserRequested || exitInfo.ThreadID != testThreadID(1) || exitInfo.ResumeHint != "amadeus --resume "+testThreadID(1).String() {
		t.Fatalf("exit info = %#v", exitInfo)
	}
	rendered := output.String()
	for _, forbidden := range []string{"?1049h", "?1049l", "?1000h", "?1002h", "?1003h"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("main-screen contract emitted %q: %q", forbidden, rendered)
		}
	}
}

func TestTranscriptSurfaceMarkdownVisualSnapshot(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.width, model.height = 80, 60
	model.insertHistoryCell(NewAgentMarkdownCell(newMarkdownSource("# Interface\n\n**Interface** provides a stable contract.\n\n- first item\n- second item\n\n| Name | Value |\n| --- | --- |\n| A | B |\n\n[docs](https://example.com/docs)\n", "/workspace")))
	view := xansi.Strip(model.View())
	for _, expected := range []string{"• # Interface", "Interface provides a stable contract.", "• first item", "• second item", "Name │ Value", "─────┼─────", "docs"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("markdown snapshot omitted %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "Interface provides a stable contract.\n  • first item") {
		t.Fatalf("markdown snapshot lost paragraph/list separation:\n%s", view)
	}
}
